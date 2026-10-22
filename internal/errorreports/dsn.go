package errorreports

import (
	"errors"
	"net/url"
	"strings"
)

// ErrInvalidDSN is returned for a DSN that cannot be used.
var ErrInvalidDSN = errors.New("invalid Sentry DSN")

// DSN is a parsed Sentry DSN: scheme://public[:secret]@host[:port][/prefix]/project.
type DSN struct {
	Raw       string
	PublicKey string
	SecretKey string
	Host      string
	// Envelope is the URL envelopes are posted to.
	Envelope string
}

// ParseDSN checks and parses a Sentry DSN.
func ParseDSN(raw string) (DSN, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User == nil ||
		u.User.Username() == "" || u.RawQuery != "" || u.Fragment != "" {
		return DSN{}, ErrInvalidDSN
	}
	path := strings.TrimSuffix(u.Path, "/")
	i := strings.LastIndexByte(path, '/')
	if i < 0 || path[i+1:] == "" {
		return DSN{}, ErrInvalidDSN
	}
	prefix, project := path[:i], path[i+1:]
	secret, _ := u.User.Password()
	endpoint := url.URL{Scheme: u.Scheme, Host: u.Host, Path: prefix + "/api/" + project + "/envelope/"}
	return DSN{
		Raw:       u.String(),
		PublicKey: u.User.Username(),
		SecretKey: secret,
		Host:      u.Host,
		Envelope:  endpoint.String(),
	}, nil
}

// authHeader is the X-Sentry-Auth header for this DSN.
func (d DSN) authHeader() string {
	h := "Sentry sentry_version=7, sentry_client=hytale-server-manager/1.0, sentry_key=" + d.PublicKey
	if d.SecretKey != "" {
		h += ", sentry_secret=" + d.SecretKey
	}
	return h
}

// authKey extracts sentry_key from an X-Sentry-Auth header.
func authKey(header string) string {
	header = strings.TrimSpace(header)
	if len(header) >= 6 && strings.EqualFold(header[:6], "sentry") {
		header = header[6:]
	}
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) == "sentry_key" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
