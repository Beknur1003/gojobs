// Package httpx is the one HTTP client every source shares. It spaces out
// requests per host and retries transient failures, so adapters stay simple.
package httpx

import (
	"context"
	"encoding/json"
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

type Client struct {
	http      *http.Client
	userAgent string
	delay     time.Duration

	mu       sync.Mutex
	nextSlot map[string]time.Time // host -> earliest time the next request may start
}

func New(userAgent string, timeout, delay time.Duration) *Client {
	return &Client{
		http:      &http.Client{Timeout: timeout},
		userAgent: userAgent,
		delay:     delay,
		nextSlot:  make(map[string]time.Time),
	}
}

// Get returns the body of a 200 response. Any other final status is an error.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("httpx.Get: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := c.wait(ctx, u.Host); err != nil {
			return nil, err
		}

		body, retryAfter, err := c.do(ctx, rawURL)
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
	return nil, fmt.Errorf("httpx.Get %s: %w", rawURL, lastErr)
}

// GetJSON decodes a 200 response into dst.
func (c *Client) GetJSON(ctx context.Context, rawURL string, dst any) error {
	body, err := c.Get(ctx, rawURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("httpx.GetJSON %s: %w", rawURL, err)
	}
	return nil
}

// do performs one request. retryAfter < 0 means the error is permanent.
func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept-Language", "ru,en;q=0.8")

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
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")), fmt.Errorf("status %d", resp.StatusCode)
	default:
		return nil, -1, fmt.Errorf("status %d", resp.StatusCode)
	}
}

// wait blocks until this host's next slot, then books the one after it.
func (c *Client) wait(ctx context.Context, host string) error {
	c.mu.Lock()
	now := time.Now()
	start := c.nextSlot[host]
	if start.Before(now) {
		start = now
	}
	c.nextSlot[host] = start.Add(c.delay)
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
