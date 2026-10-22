// Package config loads the manager configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the process-level configuration of the manager.
type Config struct {
	// ListenAddr is the address the web interface listens on.
	ListenAddr string
	// DataDir is the root of all persistent data (should be a volume).
	DataDir string
	// SecureCookies marks the session cookie as Secure (enable behind HTTPS).
	SecureCookies bool
	// TrustProxy makes the manager use X-Forwarded-For to identify clients.
	TrustProxy bool
	// MaxUploadBytes caps the size of uploaded mods and world archives.
	MaxUploadBytes int64
	// ResetPassword regenerates the panel password at startup.
	ResetPassword bool
	// BindHookLibrary is the LD_PRELOAD library telling the manager when the
	// game server binds its port (sleep mode). Ignored when missing.
	BindHookLibrary string
	// SleepTestWakeDelay postpones the server start after a wake-up, to
	// measure how long players' clients keep trying. Testing only.
	SleepTestWakeDelay time.Duration
	// MetricsStreamPort is the loopback UDP port receiving the optional
	// metrics plugin stream.
	MetricsStreamPort int
	// ErrorReportsPort is the loopback TCP port receiving the error reports
	// of the game server (Sentry-compatible).
	ErrorReportsPort int
}

// Load reads the configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr: envString("HSM_LISTEN_ADDR", ":8080"),
		DataDir:    envString("HSM_DATA_DIR", "/data"),

		BindHookLibrary: envString("HSM_BINDHOOK_LIB", "/usr/local/lib/hsm-bindhook.so"),
	}

	var err error
	if cfg.SecureCookies, err = envBool("HSM_SECURE_COOKIES", false); err != nil {
		return Config{}, err
	}
	if cfg.TrustProxy, err = envBool("HSM_TRUST_PROXY", false); err != nil {
		return Config{}, err
	}
	if cfg.ResetPassword, err = envBool("HSM_RESET_PASSWORD", false); err != nil {
		return Config{}, err
	}
	if v := envString("HSM_SLEEP_TEST_WAKE_DELAY", ""); v != "" {
		if cfg.SleepTestWakeDelay, err = time.ParseDuration(v); err != nil {
			return Config{}, fmt.Errorf("HSM_SLEEP_TEST_WAKE_DELAY: %w", err)
		}
	}
	if cfg.MetricsStreamPort, err = envInt("HSM_METRICS_STREAM_PORT", 9877); err != nil {
		return Config{}, err
	}
	if cfg.MetricsStreamPort < 1 || cfg.MetricsStreamPort > 65535 {
		return Config{}, fmt.Errorf("HSM_METRICS_STREAM_PORT must be a valid port")
	}
	if cfg.ErrorReportsPort, err = envInt("HSM_ERROR_REPORTS_PORT", 9878); err != nil {
		return Config{}, err
	}
	if cfg.ErrorReportsPort < 1 || cfg.ErrorReportsPort > 65535 {
		return Config{}, fmt.Errorf("HSM_ERROR_REPORTS_PORT must be a valid port")
	}
	maxUploadMB, err := envInt("HSM_MAX_UPLOAD_MB", 1024)
	if err != nil {
		return Config{}, err
	}
	if maxUploadMB <= 0 {
		return Config{}, fmt.Errorf("HSM_MAX_UPLOAD_MB must be positive")
	}
	cfg.MaxUploadBytes = int64(maxUploadMB) << 20
	return cfg, nil
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: invalid boolean %q", key, v)
	}
	return b, nil
}

func envInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}
