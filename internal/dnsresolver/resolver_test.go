package dnsresolver

import (
	"context"
	"net"
	"sort"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type mockClient struct {
	mock.Mock
}

func (m *mockClient) ExchangeContext(ctx context.Context, msg *dns.Msg, addr string) (*dns.Msg, time.Duration, error) {
	args := m.Called(ctx, msg, addr)
	if resp := args.Get(0); resp != nil {
		return resp.(*dns.Msg), args.Get(1).(time.Duration), args.Error(2)
	}
	return nil, args.Get(1).(time.Duration), args.Error(2)
}

type ResolverTestSuite struct {
	suite.Suite
	resolver *Client
	client   *mockClient
}

func (s *ResolverTestSuite) SetupTest() {
	s.client = new(mockClient)
	s.resolver = New(5 * time.Second)
	s.resolver.Client = s.client
}

func (s *ResolverTestSuite) TestNew() {
	testCases := []struct {
		name     string
		timeout  time.Duration
		opts     []Opt
		expected *Client
	}{
		{
			name:    "default configuration",
			timeout: 5 * time.Second,
			expected: &Client{
				Timeout: 5 * time.Second,
			},
		},
		{
			name:    "with custom resolvers",
			timeout: 5 * time.Second,
			opts: []Opt{
				WithResolvers([]string{"8.8.8.8:53", "8.8.4.4:53"}),
			},
			expected: &Client{
				Timeout:   5 * time.Second,
				Resolvers: []string{"8.8.8.8:53", "8.8.4.4:53"},
			},
		},
		{
			name:    "with custom timeout",
			timeout: 5 * time.Second,
			opts: []Opt{
				WithTimeout(10 * time.Second),
			},
			expected: &Client{
				Timeout: 10 * time.Second,
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resolver := New(tc.timeout, tc.opts...)
			s.Equal(tc.expected.Timeout, resolver.Timeout)
			s.Equal(tc.expected.Resolvers, resolver.Resolvers)
		})
	}
}

func (s *ResolverTestSuite) TestLookupHost() {
	testCases := []struct {
		name        string
		hostname    string
		setupMock   func(*mockClient)
		expected    []net.IPAddr
		expectedErr error
	}{
		{
			name:        "empty hostname",
			hostname:    "",
			expectedErr: ErrEmptyHostname,
		},
		{
			name:     "hostname is IP",
			hostname: "1.1.1.1",
			expected: []net.IPAddr{
				{IP: net.ParseIP("1.1.1.1")},
			},
		},
		{
			name:     "successful A and AAAA lookup",
			hostname: "example.com",
			setupMock: func(m *mockClient) {
				// Helper to match DNS query type
				matchQuery := func(qtype uint16) interface{} {
					return mock.MatchedBy(func(msg *dns.Msg) bool {
						return len(msg.Question) > 0 &&
							msg.Question[0].Qtype == qtype &&
							msg.Question[0].Name == dns.Fqdn("example.com")
					})
				}

				// Setup A record response
				aResp := new(dns.Msg)
				aResp.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{
							Name:   dns.Fqdn("example.com"),
							Rrtype: dns.TypeA,
							Class:  dns.ClassINET,
							Ttl:    300,
						},
						A: net.ParseIP("93.184.216.34"),
					},
				}

				// Setup AAAA record response
				aaaaResp := new(dns.Msg)
				aaaaResp.Answer = []dns.RR{
					&dns.AAAA{
						Hdr: dns.RR_Header{
							Name:   dns.Fqdn("example.com"),
							Rrtype: dns.TypeAAAA,
							Class:  dns.ClassINET,
							Ttl:    300,
						},
						AAAA: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"),
					},
				}

				// Match exact query types with specific responses
				m.On("ExchangeContext",
					mock.Anything,
					matchQuery(dns.TypeA),
					mock.Anything,
				).Return(aResp, time.Duration(0), nil)

				m.On("ExchangeContext",
					mock.Anything,
					matchQuery(dns.TypeAAAA),
					mock.Anything,
				).Return(aaaaResp, time.Duration(0), nil)
			},
			expected: []net.IPAddr{
				{IP: net.ParseIP("93.184.216.34")},
				{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
			},
		},
		{
			name:     "A lookup success, AAAA lookup failure",
			hostname: "example.com",
			setupMock: func(m *mockClient) {
				matchQuery := func(qtype uint16) interface{} {
					return mock.MatchedBy(func(msg *dns.Msg) bool {
						return len(msg.Question) > 0 &&
							msg.Question[0].Qtype == qtype &&
							msg.Question[0].Name == dns.Fqdn("example.com")
					})
				}

				aResp := new(dns.Msg)
				aResp.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{
							Name:   dns.Fqdn("example.com"),
							Rrtype: dns.TypeA,
							Class:  dns.ClassINET,
							Ttl:    300,
						},
						A: net.ParseIP("93.184.216.34"),
					},
				}

				m.On("ExchangeContext",
					mock.Anything,
					matchQuery(dns.TypeA),
					mock.Anything,
				).Return(aResp, time.Duration(0), nil)

				m.On("ExchangeContext",
					mock.Anything,
					matchQuery(dns.TypeAAAA),
					mock.Anything,
				).Return(nil, time.Duration(0), ErrNoRecords)
			},
			expected: []net.IPAddr{
				{IP: net.ParseIP("93.184.216.34")},
			},
		},
		{
			name:     "both lookups fail",
			hostname: "nonexistent.example",
			setupMock: func(m *mockClient) {
				matchQuery := func(qtype uint16) interface{} {
					return mock.MatchedBy(func(msg *dns.Msg) bool {
						return len(msg.Question) > 0 &&
							msg.Question[0].Qtype == qtype &&
							msg.Question[0].Name == dns.Fqdn("nonexistent.example")
					})
				}

				m.On("ExchangeContext",
					mock.Anything,
					matchQuery(dns.TypeA),
					mock.Anything,
				).Return(nil, time.Duration(0), ErrNoRecords)

				m.On("ExchangeContext",
					mock.Anything,
					matchQuery(dns.TypeAAAA),
					mock.Anything,
				).Return(nil, time.Duration(0), ErrNoRecords)
			},
			expectedErr: ErrNoRecords,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Reset mock for each test case
			s.SetupTest()

			if tc.setupMock != nil {
				tc.setupMock(s.client)
			}

			addrs, err := s.resolver.LookupHost(context.Background(), tc.hostname)

			if tc.expectedErr != nil {
				s.Error(err)
				s.ErrorContains(err, tc.expectedErr.Error())
				return
			}

			s.NoError(err)
			s.Equal(len(tc.expected), len(addrs))

			// Sort IPs for consistent comparison
			expectedIPs := make([]string, len(tc.expected))
			actualIPs := make([]string, len(addrs))
			for i, addr := range tc.expected {
				expectedIPs[i] = addr.IP.String()
			}
			for i, addr := range addrs {
				actualIPs[i] = addr.IP.String()
			}
			sort.Strings(expectedIPs)
			sort.Strings(actualIPs)

			s.Equal(expectedIPs, actualIPs)
			s.True(s.client.AssertExpectations(s.T()))
		})
	}
}

func (s *ResolverTestSuite) TestGetResolver() {
	testCases := []struct {
		name      string
		resolvers []string
		expected  string
	}{
		{
			name:     "no resolvers configured",
			expected: _defaultResolver,
		},
		{
			name:      "single resolver",
			resolvers: []string{"8.8.8.8:53"},
			expected:  "8.8.8.8:53",
		},
		{
			name:      "multiple resolvers",
			resolvers: []string{"8.8.8.8:53", "8.8.4.4:53"},
			expected:  "", // Will be checked differently due to randomness
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.resolver.Resolvers = tc.resolvers
			resolver := s.resolver.getResolver()

			if len(tc.resolvers) > 1 {
				s.Contains(tc.resolvers, resolver)
			} else {
				s.Equal(tc.expected, resolver)
			}
		})
	}
}

func (s *ResolverTestSuite) TestParseIPs() {
	testCases := []struct {
		name        string
		response    *dns.Msg
		expected    []net.IPAddr
		expectedErr error
	}{
		{
			name:        "nil response",
			response:    nil,
			expectedErr: ErrEmptyHostname,
		},
		{
			name: "empty answer",
			response: &dns.Msg{
				Answer: []dns.RR{},
			},
			expectedErr: ErrNoRecords,
		},
		{
			name: "valid A record",
			response: &dns.Msg{
				Answer: []dns.RR{
					&dns.A{
						A: net.ParseIP("93.184.216.34"),
					},
				},
			},
			expected: []net.IPAddr{
				{IP: net.ParseIP("93.184.216.34")},
			},
		},
		{
			name: "valid AAAA record",
			response: &dns.Msg{
				Answer: []dns.RR{
					&dns.AAAA{
						AAAA: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"),
					},
				},
			},
			expected: []net.IPAddr{
				{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
			},
		},
		{
			name: "mixed A and AAAA records",
			response: &dns.Msg{
				Answer: []dns.RR{
					&dns.A{
						A: net.ParseIP("93.184.216.34"),
					},
					&dns.AAAA{
						AAAA: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"),
					},
				},
			},
			expected: []net.IPAddr{
				{IP: net.ParseIP("93.184.216.34")},
				{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			ips, err := parseIPs(tc.response)

			if tc.expectedErr != nil {
				s.Error(err)
				s.ErrorIs(err, tc.expectedErr)
				return
			}

			s.NoError(err)
			s.Equal(len(tc.expected), len(ips))
			for i, ip := range ips {
				s.Equal(tc.expected[i].IP.String(), ip.IP.String())
			}
		})
	}
}

func TestResolverSuite(t *testing.T) {
	suite.Run(t, new(ResolverTestSuite))
}
