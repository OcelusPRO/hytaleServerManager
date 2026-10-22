package gameserver

import (
	"fmt"
	"strconv"

	"hytale-server-manager/internal/settings"
)

// Environment variables understood by the Hytale server (token passthrough).
const (
	envSessionToken   = "HYTALE_SERVER_SESSION_TOKEN"
	envIdentityToken  = "HYTALE_SERVER_IDENTITY_TOKEN"
	envDisableUpdates = "HYTALE_DISABLE_UPDATES"
)

var utf8Properties = []string{
	"-Dfile.encoding=UTF-8",
	"-Dstdin.encoding=UTF-8",
	"-Dstdout.encoding=UTF-8",
	"-Dstderr.encoding=UTF-8",
	"-Dsun.stdout.encoding=UTF-8",
	"-Dsun.stderr.encoding=UTF-8",
}

// SessionTokens are the game session credentials passed to the server.
type SessionTokens struct {
	SessionToken  string
	IdentityToken string
	OwnerUUID     string
}

// LaunchInputs gathers everything needed to build the server command line.
type LaunchInputs struct {
	JavaBin   string
	ServerJar string
	AssetsZip string
	AOTCache  string
	SaveID    string
	SaveDir   string
	HomeDir   string
	// TmpDir must allow execution: Netty extracts its native QUIC library
	// there, and /tmp is mounted noexec in the hardened container.
	TmpDir   string
	Settings settings.Settings
	Tokens   *SessionTokens // nil in offline mode
	// ExtraEnv is appended to the environment (e.g. the sleep bind hook).
	ExtraEnv []string
}

// BuildLaunchSpec turns the inputs into a process specification. Tokens go
// through the environment so they never appear in the process list.
func BuildLaunchSpec(in LaunchInputs) LaunchSpec {
	s := in.Settings
	args := []string{
		fmt.Sprintf("-Xms%dm", s.MinMemoryMB),
		fmt.Sprintf("-Xmx%dm", s.MaxMemoryMB),
		"-Djava.io.tmpdir=" + in.TmpDir,
	}
	// The container has no locale: without these, Java decodes console
	// commands and encodes its output as ASCII, mangling accents.
	args = append(args, utf8Properties...)
	if s.UseAOTCache && in.AOTCache != "" {
		args = append(args, "-XX:AOTCache="+in.AOTCache)
	}
	args = append(args,
		"-jar", in.ServerJar,
		"--assets", in.AssetsZip,
		"--bind", "0.0.0.0:"+strconv.Itoa(s.Port),
		"--auth-mode", s.AuthMode,
	)
	if s.ErrorReporting == settings.ErrorReportingOff {
		args = append(args, "--disable-sentry")
	}
	if s.AcceptEarlyPlugins {
		args = append(args, "--accept-early-plugins")
	}
	if s.NativeBackups {
		args = append(args, "--backup", "--backup-dir", "backups",
			"--backup-frequency", strconv.Itoa(s.NativeBackupMinutes))
	}

	env := []string{
		"HOME=" + in.HomeDir,
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		// Updates are handled by the manager (see install.Provisioner).
		envDisableUpdates + "=1",
	}
	env = append(env, in.ExtraEnv...)
	if in.Tokens != nil {
		args = append(args, "--owner-uuid", in.Tokens.OwnerUUID)
		env = append(env,
			envSessionToken+"="+in.Tokens.SessionToken,
			envIdentityToken+"="+in.Tokens.IdentityToken,
		)
	}
	return LaunchSpec{Path: in.JavaBin, Args: args, Dir: in.SaveDir, Env: env, SaveID: in.SaveID}
}
