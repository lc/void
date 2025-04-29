// Package dnsresolver provides DNS resolution capabilities with concurrent IPv4 and IPv6 lookups.
//
// The package implements a DNS resolver that prioritizes performance and reliability
// through concurrent queries, retries, and multiple resolver support. It's designed
// specifically for the void domain blocking system to resolve domain names to IP
// addresses efficiently.
//
// # Features
//
//   - Concurrent A and AAAA record resolution
//   - Configurable timeout and retry mechanisms
//   - Support for multiple DNS resolvers with random selection
//   - Proper error aggregation and handling
//   - Thread-safe operations
//
// # Basic Usage
//
// Create a new resolver with default settings:
//
//	resolver := dnsresolver.New(5 * time.Second)
//	ips, err := resolver.LookupHost("example.com")
//	if err != nil {
//		log.Fatal(err)
//	}
//	for _, ip := range ips {
//		fmt.Printf("Resolved IP: %s\n", ip.String())
//	}
//
// Configure resolver with custom options:
//
//	resolver := dnsresolver.New(
//		5 * time.Second,
//		dnsresolver.WithResolvers([]string{
//			"1.1.1.1:53",
//			"8.8.8.8:53",
//		}),
//		dnsresolver.WithTimeout(3 * time.Second),
//	)
//
// # Concurrent Resolution
//
// The resolver performs A and AAAA lookups concurrently:
//   - Both queries are initiated simultaneously
//   - Results are collected as they arrive
//   - Returns all successful results, even if some queries fail
//   - Aggregates errors when all queries fail
//
// # Error Handling
//
// The package defines several error types:
//   - ErrNoRecords: No DNS records found for the hostname
//   - ErrEmptyMsg: Empty DNS response received
//   - ErrEmptyHostname: Empty hostname provided
//
// Multiple errors are aggregated using go.uber.org/multierr when appropriate.
//
// # Thread Safety
//
// The resolver is safe for concurrent use. It uses appropriate synchronization
// mechanisms to protect shared resources:
//
//	// Safe for concurrent use
//	resolver := dnsresolver.New(5 * time.Second)
//	var wg sync.WaitGroup
//	for _, host := range hosts {
//		wg.Add(1)
//		go func(host string) {
//			defer wg.Done()
//			ips, err := resolver.LookupHost(host)
//			// Handle results...
//		}(host)
//	}
//	wg.Wait()
//
// # Retries and Timeouts
//
// The resolver implements a robust retry mechanism:
//   - Configurable number of retries per query
//   - Independent timeouts for each attempt
//   - Proper context cancellation handling
//
// # Implementation Notes
//
//   - Uses github.com/miekg/dns for low-level DNS operations
//   - Implements connection pooling and reuse
//   - Supports both IPv4 and IPv6 resolution
//   - Random resolver selection for load distribution
//   - Efficient handling of IP-address inputs
//
// Example configuration and usage:
//
//	resolver := dnsresolver.New(
//		5 * time.Second,
//		dnsresolver.WithResolvers([]string{
//			"1.1.1.1:53",  // Cloudflare
//			"8.8.8.8:53",  // Google
//		}),
//		dnsresolver.WithTimeout(3 * time.Second),
//	)
//
//	ctx := context.Background()
//	ips, err := resolver.LookupHost("example.com")
//	if err != nil {
//		if errors.Is(err, dnsresolver.ErrNoRecords) {
//			log.Println("No DNS records found")
//		} else {
//			log.Printf("DNS resolution failed: %v", err)
//		}
//		return
//	}
//
//	for _, ip := range ips {
//		fmt.Printf("Resolved IP: %s\n", ip.String())
//	}
//
// The package is designed to be efficient and reliable for the void
// domain blocking system's needs while remaining general enough for
// other DNS resolution requirements.
package dnsresolver
