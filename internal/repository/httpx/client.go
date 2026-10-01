// Package httpx is the one HTTP client every source shares. It spaces out
// requests per host and retries transient failures, so adapters stay simple.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	maxBody    = 48 << 20 // Greenhouse boards with full descriptions reach tens of MB
	maxRetries = 3
)

// ErrNotFound is a 404: a renamed channel or a company that left its ATS.
// Callers treat it as "this source is gone", not as a failure to retry.
var ErrNotFound = errors.New("not found")

type Client struct {
	http      *http.Client
	userAgent string
	delay     time.Duration
	// hostDelay overrides delay for hosts built for machine traffic (job
	// board APIs), so a thousand boards do not take an hour.
	hostDelay map[string]time.Duration

	mu       sync.Mutex
	nextSlot map[string]time.Time // host -> earliest time the next request may start
}

func New(userAgent string, timeout, delay time.Duration, hostDelay map[string]time.Duration) *Client {
	return &Client{
		http:      &http.Client{Timeout: timeout},
		userAgent: userAgent,
		delay:     delay,
		hostDelay: hostDelay,
		nextSlot:  make(map[string]time.Time),
	}
}

// Get returns the body of a 200 response. Any other final status is an error.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	return c.send(ctx, http.MethodGet, rawURL, nil, false)
}

// GetJSON decodes a 200 response into dst.
func (c *Client) GetJSON(ctx context.Context, rawURL string, dst any) error {
	body, err := c.send(ctx, http.MethodGet, rawURL, nil, true)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("httpx.GetJSON %s: %w", rawURL, err)
	}
	return nil
}

// PostJSON sends payload as JSON and decodes a 200 response into dst.
func (c *Client) PostJSON(ctx context.Context, rawURL string, payload, dst any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("httpx.PostJSON: %w", err)
	}
	body, err := c.send(ctx, http.MethodPost, rawURL, raw, true)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("httpx.PostJSON %s: %w", rawURL, err)
	}
	return nil
}

// send runs one request with retries. wantJSON asks for JSON explicitly: some
// APIs (Workday) answer the same URL with HTML otherwise.
func (c *Client) send(ctx context.Context, method, rawURL string, payload []byte, wantJSON bool) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("httpx: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := c.wait(ctx, u.Host); err != nil {
			return nil, err
		}

		body, retryAfter, err := c.do(ctx, method, rawURL, payload, wantJSON)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if retryAfter < 0 {
			break // not retryable
		}

		backoff := retryAfter
		if backoff == 0 {
			backoff = time.Duration(attempt+1) * 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("httpx %s %s: %w", method, rawURL, lastErr)
}

// do performs one request. retryAfter < 0 means the error is permanent.
func (c *Client) do(ctx context.Context, method, rawURL string, payload []byte, wantJSON bool) (body []byte, retryAfter time.Duration, err error) {
	var reqBody io.Reader
	if payload != nil {
		reqBody = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reqBody)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept-Language", "ru,en;q=0.8")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if wantJSON {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, -1, ctx.Err()
		}
		return nil, 0, err // network blip: retry
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, 0, err
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, 0, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, -1, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")), fmt.Errorf("status %d", resp.StatusCode)
	default:
		return nil, -1, fmt.Errorf("status %d", resp.StatusCode)
	}
}

// wait blocks until this host's next slot, then books the one after it.
func (c *Client) wait(ctx context.Context, host string) error {
	delay, ok := c.hostDelay[host]
	if !ok {
		delay = c.delay
	}

	c.mu.Lock()
	now := time.Now()
	start := c.nextSlot[host]
	if start.Before(now) {
		start = now
	}
	c.nextSlot[host] = start.Add(delay)
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(start)):
		return nil
	}
}

func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	return min(time.Duration(secs)*time.Second, time.Minute)
}
