package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// publicRoutes are the only API routes reachable without a session.
var publicRoutes = map[string]bool{
	"POST /api/login":                true,
	"GET /api/session":               true,
	"GET /api/login/methods":         true,
	"POST /api/login/passkey/begin":  true,
	"POST /api/login/passkey/finish": true,
}

var routePattern = regexp.MustCompile(`HandleFunc\("([A-Z]+) (/api/[^"]*)"`)
var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// TestEveryRouteRequiresASession reads the routes declared in this package
// so that a route added later is checked too.
func TestEveryRouteRequiresASession(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routePattern.FindAllStringSubmatch(string(src), -1) {
			routes = append(routes, m[1]+" "+m[2])
		}
	}
	if len(routes) < 50 {
		t.Fatalf("only %d routes found: the pattern no longer matches the code", len(routes))
	}

	srv := newTestServer(t)
	anon := &client{t: t, base: srv.URL}
	for _, route := range routes {
		if publicRoutes[route] {
			continue
		}
		method, path, _ := strings.Cut(route, " ")
		// With the CSRF header, only the missing session can reject it.
		resp := anon.do(method, pathParam.ReplaceAllString(path, "x"), "", ajax)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a session: HTTP %d, want 401", route, resp.StatusCode)
		}
	}
	// An unknown path below /api is not a way around the check either.
	if resp := anon.do("GET", "/api/does-not-exist", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown path: HTTP %d", resp.StatusCode)
	}
	if resp := anon.do("GET", "/api/login/../players", "", nil); resp.StatusCode == http.StatusOK {
		t.Errorf("path traversal reached a private route")
	}
}
