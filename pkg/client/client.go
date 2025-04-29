// Package client is a thin convenience wrapper for CLI tools to call the
// Void daemon’s JSON API over a Unix‑domain socket. It re‑exports the DTOs
// from pkg/api so callers get strongly‑typed results instead of generic maps.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/lc/void/internal/rules"
	"github.com/lc/void/pkg/api"
)

// Client holds an http.Client wired to a Unix socket.
type Client struct {
	hc   *http.Client
	base string // dummy scheme+host for Request.URL (http://unix)
}

// New returns a Client that dials the given Unix‑domain socket path.
func New(socketPath string) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}
	tr := &http.Transport{DialContext: dial}
	return &Client{hc: &http.Client{Transport: tr}, base: "http://unix"}
}

// --------------------------- commands ------------------------------

// Block sends a request to block the specified domain.
// If ttl is 0, the domain will be blocked permanently.
func (c *Client) Block(ctx context.Context, domain string, ttl time.Duration) error {
	req := api.BlockRequest{Domain: domain, TTL: ttl}
	return c.post(ctx, "/v1/block", req)
}

// Unblock sends a request to unblock the rule with the specified ID.
func (c *Client) Unblock(ctx context.Context, id string) error {
	req := api.UnblockRequest{ID: id}
	return c.post(ctx, "/v1/unblock", req)
}

// Status retrieves the current status of the daemon.
// It returns information about the number of rules, uptime, and version.
func (c *Client) Status(ctx context.Context) (api.StatusResponse, error) {
	var out api.StatusResponse
	err := c.get(ctx, "/v1/status", &out)
	return out, err
}

// Rules retrieves the current list of rules from the daemon.
func (c *Client) Rules(ctx context.Context) ([]rules.Rule, error) {
	var out []rules.Rule
	err := c.get(ctx, "/v1/rules", &out)
	return out, err
}

// --------------------------- HTTP helpers --------------------------

func (c *Client) post(ctx context.Context, path string, payload any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
