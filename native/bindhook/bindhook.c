// hsm-bindhook: hands the game port over from the manager to the server.
//
// Loaded into the Java process with LD_PRELOAD while the sleep mode is
// enabled. While the server boots, the manager keeps the game port and
// leaves the first packets of the players' clients unread in the socket
// queue (Hytale clients send a single packet per connection attempt and
// never retransmit it). When the server binds the port, this bind() wrapper
// asks the manager what to do:
//
//   "fd"  the manager passes its own socket (SCM_RIGHTS): it replaces the
//         server's socket, queued packets included, as if the server had
//         bound the port itself and received them;
//   "ok"  the manager released the port (or holds nothing): real bind().
//
// Replacing the socket keeps the server's view consistent: the options it
// set before bind() are copied to the new socket, and its epoll
// registration (made before bind() by Netty) is replayed, which is why
// epoll_ctl() is wrapped too. The replay waits for the manager's "go" on
// the same connection, sent once the server has booted: the server turns
// players away while booting, so it must not read the queued packets
// before.
//
// Environment:
//   HSM_BINDHOOK_SOCK  Unix socket of the manager
//   HSM_BINDHOOK_PORT  game port to watch
//
// Fail-open: without the variables, if the manager does not answer in time
// or if anything goes wrong, the real bind() is performed as if the library
// was not there.

#define _GNU_SOURCE
#include <arpa/inet.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <netinet/udp.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>

#ifndef UDP_SEGMENT
#define UDP_SEGMENT 103
#endif
#ifndef UDP_GRO
#define UDP_GRO 104
#endif

typedef int (*bind_fn)(int, const struct sockaddr *, socklen_t);
typedef int (*epoll_ctl_fn)(int, int, int, struct epoll_event *);

static bind_fn real_bind;
static epoll_ctl_fn real_epoll_ctl;
static int active; // the hook variables are set

static void init(void) __attribute__((constructor));
static void init(void) {
    real_bind = (bind_fn)dlsym(RTLD_NEXT, "bind");
    real_epoll_ctl = (epoll_ctl_fn)dlsym(RTLD_NEXT, "epoll_ctl");
    active = getenv("HSM_BINDHOOK_SOCK") != NULL && getenv("HSM_BINDHOOK_PORT") != NULL;
}

// ---- epoll registrations, replayed on a replaced socket ----
//
// The replaced socket is registered with epoll only once the manager says
// the server has booted: until then the server does not see the queued
// packets (it would turn players away while booting). Meanwhile its
// epoll_ctl() calls on that socket are recorded instead of being applied.

#define MAX_TRACKED_FD 65536

struct registration {
    int epfd; // 0 when none
    struct epoll_event event;
};

static struct registration registrations[MAX_TRACKED_FD];
static int deferred_fd = -1; // replaced socket not yet registered
static pthread_mutex_t registrations_lock = PTHREAD_MUTEX_INITIALIZER;

int epoll_ctl(int epfd, int op, int fd, struct epoll_event *event) {
    if (real_epoll_ctl == NULL) {
        real_epoll_ctl = (epoll_ctl_fn)dlsym(RTLD_NEXT, "epoll_ctl");
    }
    if (!active || fd < 0 || fd >= MAX_TRACKED_FD) {
        return real_epoll_ctl(epfd, op, fd, event);
    }
    pthread_mutex_lock(&registrations_lock);
    int deferred = fd == deferred_fd;
    int result = deferred ? 0 : real_epoll_ctl(epfd, op, fd, event);
    if (result == 0) {
        if (op == EPOLL_CTL_DEL) {
            registrations[fd].epfd = 0;
        } else if (event != NULL) {
            registrations[fd].epfd = epfd;
            registrations[fd].event = *event;
        }
    }
    pthread_mutex_unlock(&registrations_lock);
    return result;
}

static void defer_registration(int fd) {
    pthread_mutex_lock(&registrations_lock);
    deferred_fd = fd;
    pthread_mutex_unlock(&registrations_lock);
}

// replay_registration registers the replaced socket as the server last asked.
static void replay_registration(int fd) {
    pthread_mutex_lock(&registrations_lock);
    if (deferred_fd == fd) {
        deferred_fd = -1;
    }
    struct registration r = registrations[fd];
    if (r.epfd != 0) {
        // The previous socket left the epoll set when it was closed by dup3().
        if (real_epoll_ctl(r.epfd, EPOLL_CTL_ADD, fd, &r.event) != 0 && errno == EEXIST) {
            real_epoll_ctl(r.epfd, EPOLL_CTL_MOD, fd, &r.event);
        }
    }
    pthread_mutex_unlock(&registrations_lock);
}

struct release {
    int conn; // connection to the manager
    int fd;   // replaced socket
};

// await_release waits for the manager's "go" (server booted), or for the
// connection to end, then lets the server read the socket.
static void *await_release(void *arg) {
    struct release r = *(struct release *)arg;
    free(arg);
    struct timeval timeout = {120, 0};
    setsockopt(r.conn, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
    char buf[16];
    (void)read(r.conn, buf, sizeof(buf));
    close(r.conn);
    replay_registration(r.fd);
    return NULL;
}

static void release_later(int conn, int fd) {
    struct release *r = malloc(sizeof(*r));
    pthread_t thread;
    if (r != NULL) {
        r->conn = conn;
        r->fd = fd;
        if (pthread_create(&thread, NULL, await_release, r) == 0) {
            pthread_detach(thread);
            return;
        }
        free(r);
    }
    close(conn);
    replay_registration(fd);
}

// ---- helpers ----

static int bound_port(const struct sockaddr *addr, socklen_t len, int *any) {
    *any = 0;
    if (addr == NULL) {
        return -1;
    }
    if (addr->sa_family == AF_INET && len >= sizeof(struct sockaddr_in)) {
        const struct sockaddr_in *in = (const struct sockaddr_in *)addr;
        *any = in->sin_addr.s_addr == htonl(INADDR_ANY);
        return ntohs(in->sin_port);
    }
    if (addr->sa_family == AF_INET6 && len >= sizeof(struct sockaddr_in6)) {
        const struct sockaddr_in6 *in6 = (const struct sockaddr_in6 *)addr;
        *any = memcmp(&in6->sin6_addr, &in6addr_any, sizeof(in6addr_any)) == 0;
        return ntohs(in6->sin6_port);
    }
    return -1;
}

static int int_option(int fd, int level, int name) {
    int value = 0;
    socklen_t size = sizeof(value);
    if (getsockopt(fd, level, name, &value, &size) != 0) {
        return -1;
    }
    return value;
}

// copy_options gives the new socket the options the server set on its own
// socket before bind().
static void copy_options(int from, int to) {
    static const int options[][2] = {
        {SOL_SOCKET, SO_REUSEADDR},
        {SOL_SOCKET, SO_REUSEPORT},
        {SOL_SOCKET, SO_BROADCAST},
        {SOL_SOCKET, SO_PRIORITY},
        {SOL_SOCKET, SO_TIMESTAMP},
        {SOL_SOCKET, SO_TIMESTAMPNS},
        {SOL_SOCKET, SO_RXQ_OVFL},
        {IPPROTO_IP, IP_TOS},
        {IPPROTO_IP, IP_TTL},
        {IPPROTO_IP, IP_PKTINFO},
        {IPPROTO_IP, IP_RECVERR},
        {IPPROTO_IP, IP_RECVTTL},
        {IPPROTO_IP, IP_RECVTOS},
        {IPPROTO_IP, IP_MTU_DISCOVER},
        {IPPROTO_IP, IP_RECVORIGDSTADDR},
        {IPPROTO_UDP, UDP_SEGMENT},
        {IPPROTO_UDP, UDP_GRO},
    };
    for (size_t i = 0; i < sizeof(options) / sizeof(options[0]); i++) {
        int value = int_option(from, options[i][0], options[i][1]);
        if (value >= 0 && value != int_option(to, options[i][0], options[i][1])) {
            setsockopt(to, options[i][0], options[i][1], &value, sizeof(value));
        }
    }
    // The kernel reports twice the requested buffer sizes.
    static const int buffers[] = {SO_RCVBUF, SO_SNDBUF};
    for (size_t i = 0; i < 2; i++) {
        int value = int_option(from, SOL_SOCKET, buffers[i]);
        if (value > 0) {
            value /= 2;
            setsockopt(to, SOL_SOCKET, buffers[i], &value, sizeof(value));
        }
    }
}

// adopt replaces the socket fd by the manager's socket. It reports success;
// on failure fd is left untouched.
static int adopt(int fd, int received, int port) {
    if (int_option(received, SOL_SOCKET, SO_DOMAIN) != int_option(fd, SOL_SOCKET, SO_DOMAIN) ||
        int_option(received, SOL_SOCKET, SO_TYPE) != SOCK_DGRAM) {
        return 0;
    }
    struct sockaddr_storage local;
    socklen_t size = sizeof(local);
    int any;
    if (getsockname(received, (struct sockaddr *)&local, &size) != 0 ||
        bound_port((struct sockaddr *)&local, size, &any) != port) {
        return 0;
    }
    copy_options(fd, received);

    int status = fcntl(fd, F_GETFL);
    int descriptor = fcntl(fd, F_GETFD);
    if (status < 0 || descriptor < 0 || fcntl(received, F_SETFL, status) != 0) {
        return 0;
    }
    defer_registration(fd);
    if (dup3(received, fd, (descriptor & FD_CLOEXEC) ? O_CLOEXEC : 0) < 0) {
        replay_registration(fd); // fd unchanged: restore the server's view
        return 0;
    }
    return 1; // registered later, see release_later
}

// ask tells the manager about the bind and returns the socket it passes,
// or -1 when it answers "ok" (or not at all).
static int ask(int s, const char *path, const char *request) {
    struct sockaddr_un addr;
    if (strlen(path) >= sizeof(addr.sun_path)) {
        return -1;
    }
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    strcpy(addr.sun_path, path);
    if (connect(s, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        return -1;
    }
    int n = strlen(request);
    if (write(s, request, n) != n) {
        return -1;
    }

    char reply[16] = {0};
    union {
        char buf[CMSG_SPACE(sizeof(int))];
        struct cmsghdr align;
    } control;
    struct iovec iov = {reply, sizeof(reply) - 1};
    struct msghdr hdr = {0};
    hdr.msg_iov = &iov;
    hdr.msg_iovlen = 1;
    hdr.msg_control = control.buf;
    hdr.msg_controllen = sizeof(control.buf);
    if (recvmsg(s, &hdr, MSG_CMSG_CLOEXEC) <= 0) {
        return -1;
    }
    for (struct cmsghdr *c = CMSG_FIRSTHDR(&hdr); c != NULL; c = CMSG_NXTHDR(&hdr, c)) {
        if (c->cmsg_level == SOL_SOCKET && c->cmsg_type == SCM_RIGHTS) {
            int received;
            memcpy(&received, CMSG_DATA(c), sizeof(int));
            return received;
        }
    }
    return -1;
}

// wait_closed waits until the manager closed the connection, which it does
// after closing its own copy of the socket: the port is then free.
static void wait_closed(int s) {
    char buf[16];
    while (read(s, buf, sizeof(buf)) > 0) {
    }
}

int bind(int fd, const struct sockaddr *addr, socklen_t len) {
    if (real_bind == NULL) {
        init();
        if (real_bind == NULL) {
            errno = ENOSYS;
            return -1;
        }
    }
    int any;
    int port = bound_port(addr, len, &any);
    if (!active || port <= 0 || port != atoi(getenv("HSM_BINDHOOK_PORT")) ||
        int_option(fd, SOL_SOCKET, SO_TYPE) != SOCK_DGRAM) {
        return real_bind(fd, addr, len);
    }

    int saved = errno;
    int domain = int_option(fd, SOL_SOCKET, SO_DOMAIN);
    int v6only = domain == AF_INET6 ? int_option(fd, IPPROTO_IPV6, IPV6_V6ONLY) : 0;
    // "bind <port> <domain> <v6only> <any> <reuseaddr> <reuseport>"
    char request[96];
    snprintf(request, sizeof(request), "bind %d %d %d %d %d %d\n", port, domain, v6only > 0, any,
             int_option(fd, SOL_SOCKET, SO_REUSEADDR) > 0, int_option(fd, SOL_SOCKET, SO_REUSEPORT) > 0);
    int s = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (s >= 0) {
        struct timeval timeout = {5, 0};
        setsockopt(s, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
        setsockopt(s, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout));
        int received = ask(s, getenv("HSM_BINDHOOK_SOCK"), request);
        if (received >= 0) {
            int adopted = adopt(fd, received, port);
            close(received);
            if (adopted) {
                release_later(s, fd); // takes over s
                errno = saved;
                return 0; // bound: the manager's socket now lives in fd
            }
        }
        wait_closed(s);
        close(s);
    }
    errno = saved;
    return real_bind(fd, addr, len);
}
