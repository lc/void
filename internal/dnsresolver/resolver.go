// Package dnsresolver provides DNS resolution functionality for domain names.
// It supports concurrent resolution of IPv4 and IPv6 addresses with retries
// and configurable timeouts.
package dnsresolver

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/multierr"
	"golang.org/x/sync/errgroup"
)

var (
	// ErrNoRecords is returned when no DNS records are found for a hostname.
	ErrNoRecords = fmt.Errorf("no records found")
	// ErrEmptyMsg is returned when the DNS response message is empty.
	ErrEmptyMsg = fmt.Errorf("empty message")
	// ErrEmptyHostname is returned when an empty hostname is provided.
	ErrEmptyHostname = fmt.Errorf("empty hostname")
)

var _defaultResolver = "1.1.1.1:53"

var _ Clienter = (*Client)(nil)

// Clienter defines the interface for DNS resolution.
type Clienter interface {
	// LookupHost resolves a hostname to IPv4 & IPv6 addresses.
	LookupHost(ctx context.Context, hostname string) ([]net.IPAddr, error)
}

// Exchanger defines the interface for DNS message exchange.
type Exchanger interface {
	ExchangeContext(ctx context.Context, m *dns.Msg, a string) (r *dns.Msg, rtt time.Duration, err error)
}

// Client implements the Clienter interface for DNS resolution.
type Client struct {
	Client    Exchanger
	Timeout   time.Duration
	Resolvers []string
	Retries   uint

	mu sync.Mutex
}

// Opt is a function option for configuring the Client.
type Opt func(r *Client)

// New creates a new Client with the given timeout and optional configurations.
// The returned Client is ready to use for DNS lookups.
func New(timeout time.Duration, opts ...Opt) *Client {
	res := &Client{
		Client: &dns.Client{
			Timeout: timeout,
		},
		Timeout: timeout,
	}

	for _, o := range opts {
		o(res)
	}

	return res
}

// WithResolvers returns an option to set custom DNS resolvers.
// If not provided, the default resolver (1.1.1.1:53) will be used.
func WithResolvers(resolvers []string) Opt {
	return func(r *Client) {
		r.Resolvers = resolvers
	}
}

// WithTimeout returns an option to set a custom timeout for DNS queries.
// This overrides the timeout provided to New.
func WithTimeout(timeout time.Duration) Opt {
	return func(r *Client) {
		r.Timeout = timeout
	}
}

// LookupHost resolves a hostname to a slice of IP addresses.
// It handles both IPv4 and IPv6 addresses and returns them as net.IPAddr.
// If the hostname is already an IP address, it returns it directly.
// Returns an error if the hostname is empty or if DNS resolution fails.
func (r *Client) LookupHost(ctx context.Context, hostname string) ([]net.IPAddr, error) {
	var (
		addr []net.IPAddr
		err  error
	)

	// ensure we have a hostname
	if strings.TrimSpace(hostname) == "" {
		return nil, ErrEmptyHostname
	}

	// if hostname is an IP, return it as is.
	if ip := net.ParseIP(hostname); ip != nil {
		addr = append(addr, net.IPAddr{IP: ip})
		return addr, nil
	}

	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	addr, err = r.lookupIPs(ctx, hostname)
	if err != nil {
		return nil, err
	}

	return addr, nil
}

// lookupIPs resolves A and AAAA records concurrently.
// It returns every address that succeeded, or an aggregated
// error if *both* queries fail.
func (r *Client) lookupIPs(ctx context.Context, host string) ([]net.IPAddr, error) {
	grp, ctx := errgroup.WithContext(ctx)

	var (
		ips  []net.IPAddr
		errs error
	)

	for _, qt := range [...]uint16{dns.TypeA, dns.TypeAAAA} {
		qt := qt // capture loop variable per Uber guidance

		grp.Go(func() error {
			addrs, err := r.lookup(ctx, host, qt)
			r.mu.Lock()
			defer r.mu.Unlock()

			if err != nil {
				errs = multierr.Append(errs, err) // collect but don’t cancel peer
				return nil
			}
			ips = append(ips, addrs...)
			return nil
		})
	}

	// Wait for both goroutines.
	if err := grp.Wait(); err != nil {
		// If errgroup returns an error, include it with our aggregated errors
		errs = multierr.Append(errs, err)
	}

	if len(ips) == 0 {
		// Both lookups failed – return the aggregated error list.
		return nil, fmt.Errorf("dns lookup for %q: %w", host, errs)
	}
	return ips, nil
}

// lookup resolves qtype (A, AAAA, …) for host and returns the parsed
// IP answers. It retries r.Retries additional times before giving up.
func (r *Client) lookup(ctx context.Context, host string, qtype uint16) ([]net.IPAddr, error) {
	var lastErr error
	for attempt := uint(0); attempt <= r.Retries; attempt++ {
		// check if caller cancellation
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Fresh request each attempt: ExchangeContext mutates *dns.Msg
		domain := dns.Fqdn(host)
		req := &dns.Msg{}
		req.SetQuestion(domain, qtype)

		resp, _, err := r.Client.ExchangeContext(ctx, req, r.getResolver())
		if err != nil {
			lastErr = err
			continue // retry
		}
		if resp == nil {
			return nil, ErrEmptyMsg
		}

		ips, err := parseIPs(resp)
		if err != nil {
			lastErr = err
			continue // retry
		}
		return ips, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("dns lookup failed for %q", host)
	}
	return nil, lastErr
}

// parseIPs parses the DNS response and returns a slice of IPv4 & v6 addresses.
func parseIPs(resp *dns.Msg) ([]net.IPAddr, error) {
	if resp == nil {
		return nil, ErrEmptyHostname
	}

	var ips []net.IPAddr
	for _, r := range resp.Answer {
		switch record := r.(type) {
		case *dns.A:
			ips = append(ips, net.IPAddr{IP: record.A})
		case *dns.AAAA:
			ips = append(ips, net.IPAddr{IP: record.AAAA})
		}
	}

	if len(ips) == 0 {
		return nil, ErrNoRecords
	}

	return ips, nil
}

// getResolver returns a random resolver from the list of resolvers.
func (r *Client) getResolver() string {
	if len(r.Resolvers) == 0 {
		return _defaultResolver
	}

	// Use crypto/rand for secure random selection
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(r.Resolvers))))
	if err != nil {
		// Fall back to first resolver on error
		return r.Resolvers[0]
	}

	return r.Resolvers[n.Int64()]
}
