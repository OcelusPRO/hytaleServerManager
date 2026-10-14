// Package install downloads everything the game server needs at runtime:
// the Java runtime and the game files, and keeps them up to date.
package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"os"
	"strings"
)

// Fetcher performs HTTP downloads with integrity and size checks.
type Fetcher struct {
	client *http.Client
}

// NewFetcher returns a Fetcher using client.
func NewFetcher(client *http.Client) *Fetcher {
	return &Fetcher{client: client}
}

// JSON fetches url and decodes the JSON body into v.
func (f *Fetcher) JSON(ctx context.Context, url string, v any) error {
	resp, err := f.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

// ProgressFunc receives the fraction (0..1) of a download already received.
type ProgressFunc func(fraction float64)

// ToFile downloads url into dst. When expectedSHA256 is set, the content
// must match it or the file is removed. onProgress may be nil.
func (f *Fetcher) ToFile(ctx context.Context, url, dst, expectedSHA256 string, maxBytes int64, onProgress ProgressFunc) error {
	resp, err := f.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	hash := sha256.New()
	counter := &progressWriter{total: resp.ContentLength, report: onProgress}
	n, err := io.Copy(io.MultiWriter(out, hash, counter), io.LimitReader(resp.Body, maxBytes+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
	case n > maxBytes:
		err = fmt.Errorf("download of %s exceeds %d bytes", redact(url), maxBytes)
	case expectedSHA256 != "" && !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256):
		err = fmt.Errorf("checksum mismatch for %s", redact(url))
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

func (f *Fetcher) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", redact(url), errors.Unwrap(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: HTTP %d", redact(url), resp.StatusCode)
	}
	return resp, nil
}

// progressWriter reports download progress at most once per percent.
type progressWriter struct {
	total   int64
	written int64
	last    int
	report  ProgressFunc
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if p.report != nil && p.total > 0 {
		if pct := int(p.written * 100 / p.total); pct != p.last {
			p.last = pct
			p.report(float64(p.written) / float64(p.total))
		}
	}
	return len(b), nil
}

// redact strips the query string (signatures of signed URLs) from errors.
func redact(raw string) string {
	u, err := nurl.Parse(raw)
	if err != nil {
		return "URL invalide"
	}
	u.RawQuery = ""
	return u.String()
}
