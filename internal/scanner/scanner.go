// Package scanner checks destination URLs against Google Safe Browsing
// (v5 urls:search), with a fake for local/tests and a noop for environments
// where scanning is disabled (no NAT egress).
package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
)

// Verdict is the scan result for one URL.
type Verdict struct {
	Unsafe  bool
	Threats []string
}

// ErrDisabled is returned by Noop: scanning is turned off in this environment.
var ErrDisabled = errors.New("url scanning disabled")

// MaxBatch is the Safe Browsing v5 urls:search limit per request.
const MaxBatch = 50

// Noop never scans.
type Noop struct{}

// Check implements links.Scanner.
func (Noop) Check(context.Context, []string) (map[string]Verdict, error) { return nil, ErrDisabled }

// FakeMarker is the host fragment the fake scanner flags (SPEC §15).
const FakeMarker = "malware.testing.google.test"

// Fake flags any URL containing FakeMarker.
type Fake struct{}

// Check implements links.Scanner.
func (Fake) Check(_ context.Context, urls []string) (map[string]Verdict, error) {
	out := make(map[string]Verdict, len(urls))
	for _, u := range urls {
		if strings.Contains(u, FakeMarker) {
			out[u] = Verdict{Unsafe: true, Threats: []string{"MALWARE"}}
		} else {
			out[u] = Verdict{}
		}
	}
	return out, nil
}

// SafeBrowsing calls GET /v5/urls:search. The API key is sent in the
// X-Goog-Api-Key header so it never appears in a URL or access log.
type SafeBrowsing struct {
	endpoint string
	key      config.Secret
	client   *http.Client
}

// NewSafeBrowsing returns a client for endpoint (e.g. https://safebrowsing.googleapis.com).
func NewSafeBrowsing(endpoint string, key config.Secret, client *http.Client) *SafeBrowsing {
	return &SafeBrowsing{endpoint: strings.TrimSuffix(endpoint, "/"), key: key, client: client}
}

type searchResponse struct {
	Threats []struct {
		URL         string   `json:"url"`
		ThreatTypes []string `json:"threatTypes"`
	} `json:"threats"`
}

// Check looks up urls in batches of MaxBatch. Every input URL gets a verdict.
func (s *SafeBrowsing) Check(ctx context.Context, urls []string) (map[string]Verdict, error) {
	out := make(map[string]Verdict, len(urls))
	for start := 0; start < len(urls); start += MaxBatch {
		batch := urls[start:min(start+MaxBatch, len(urls))]
		if err := s.checkBatch(ctx, batch, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *SafeBrowsing) checkBatch(ctx context.Context, batch []string, out map[string]Verdict) error {
	resp, err := s.search(ctx, batch)
	if err != nil {
		return err
	}
	requested := make(map[string]bool, len(batch))
	for _, u := range batch {
		requested[u] = true
		out[u] = Verdict{}
	}
	unmatched := false
	for _, t := range resp.Threats {
		switch {
		case requested[t.URL]:
			out[t.URL] = Verdict{Unsafe: true, Threats: t.ThreatTypes}
		case len(batch) == 1:
			// Matches can be host-suffix/path-prefix expressions of the URL.
			out[batch[0]] = Verdict{Unsafe: true, Threats: t.ThreatTypes}
		default:
			unmatched = true
		}
	}
	if unmatched {
		// A threat we cannot attribute: re-check each URL on its own (conservative).
		for _, u := range batch {
			if out[u].Unsafe {
				continue
			}
			if err := s.checkBatch(ctx, []string{u}, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SafeBrowsing) search(ctx context.Context, batch []string) (searchResponse, error) {
	q := url.Values{}
	for _, u := range batch {
		q.Add("urls", u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+"/v5/urls:search?"+q.Encode(), nil)
	if err != nil {
		return searchResponse{}, fmt.Errorf("safe browsing request: %w", err)
	}
	req.Header.Set("X-Goog-Api-Key", s.key.Reveal())
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return searchResponse{}, fmt.Errorf("safe browsing: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return searchResponse{}, fmt.Errorf("safe browsing: unexpected status %d", resp.StatusCode)
	}
	var sr searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&sr); err != nil {
		return searchResponse{}, fmt.Errorf("safe browsing: decode response: %w", err)
	}
	return sr, nil
}

// redactURLError drops the request URL (which contains the scanned URLs) from
// transport errors so they are not logged verbatim.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
