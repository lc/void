package pf

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lc/void/internal/mocks"

	"github.com/stretchr/testify/assert"

	"github.com/lc/void/internal/rules"

	"github.com/stretchr/testify/suite"
)

type PFTestSuite struct {
	suite.Suite
}

func (s *PFTestSuite) TestRunParseRules() {
	tests := []struct {
		name          string
		in            string
		expectedRules int
		expected      []rules.Rule
		expectErr     bool
		err           error
	}{
		{
			name: "one rule parsed",
			in: `# void-anchor
# Options
set block-policy drop
set fingerprints "/etc/pf.os"
set ruleset-optimization basic
set skip on lo0

# void ruleset for blocking sites
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e BEGIN ===
# Domain: example.com
# Expires: 2025-04-28T14:10:36-05:00
block return out proto tcp from any to 23.192.228.84
block return out proto udp from any to 23.192.228.84
block return out proto tcp from any to 23.215.0.138
block return out proto udp from any to 23.215.0.138
block return out proto tcp from any to 96.7.128.198
block return out proto udp from any to 96.7.128.198
block return out proto tcp from any to 23.192.228.80
block return out proto udp from any to 23.192.228.80
block return out proto tcp from any to 96.7.128.175
block return out proto udp from any to 96.7.128.175
block return out proto tcp from any to 23.215.0.136
block return out proto udp from any to 23.215.0.136
block return out proto tcp from any to 2600:1406:bc00:53::b81e:94ce
block return out proto udp from any to 2600:1406:bc00:53::b81e:94ce
block return out proto tcp from any to 2600:1408:ec00:36::1736:7f31
block return out proto udp from any to 2600:1408:ec00:36::1736:7f31
block return out proto tcp from any to 2600:1408:ec00:36::1736:7f24
block return out proto udp from any to 2600:1408:ec00:36::1736:7f24
block return out proto tcp from any to 2600:1406:3a00:21::173e:2e66
block return out proto udp from any to 2600:1406:3a00:21::173e:2e66
block return out proto tcp from any to 2600:1406:3a00:21::173e:2e65
block return out proto udp from any to 2600:1406:3a00:21::173e:2e65
block return out proto tcp from any to 2600:1406:bc00:53::b81e:94c8
block return out proto udp from any to 2600:1406:bc00:53::b81e:94c8
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e END ===
`,
			expectedRules: 1,
			expected: []rules.Rule{
				{
					ID:     "ecceadd1-d9ca-4ec9-a906-0e3e4736a45e",
					Domain: "example.com",
					IPs: []net.IPAddr{
						{IP: net.ParseIP("23.192.228.84")},
						{IP: net.ParseIP("23.215.0.138")},
						{IP: net.ParseIP("96.7.128.198")},
						{IP: net.ParseIP("23.192.228.80")},
						{IP: net.ParseIP("96.7.128.175")},
						{IP: net.ParseIP("23.215.0.136")},
						{IP: net.ParseIP("2600:1406:bc00:53::b81e:94ce")},
						{IP: net.ParseIP("2600:1408:ec00:36::1736:7f31")},
						{IP: net.ParseIP("2600:1408:ec00:36::1736:7f24")},
						{IP: net.ParseIP("2600:1406:3a00:21::173e:2e66")},
						{IP: net.ParseIP("2600:1406:3a00:21::173e:2e65")},
						{IP: net.ParseIP("2600:1406:bc00:53::b81e:94c8")},
					},
					Expires:    mustParseTime(time.RFC3339, "2025-04-28T14:10:36-05:00"),
					Permanent:  false,
					ResolvedAt: time.Time{},
				},
			},
			expectErr: false,
		},
		{
			name: "one rule parsed, no expires",
			in: `# void-anchor
# Options
set block-policy drop
set fingerprints "/etc/pf.os"
set ruleset-optimization basic
set skip on lo0

# void ruleset for blocking sites
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e BEGIN ===
# Domain: example.com
block return out proto tcp from any to 23.192.228.84
block return out proto udp from any to 23.192.228.84
block return out proto tcp from any to 23.215.0.138
block return out proto udp from any to 23.215.0.138
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e END ===
`,
			expectedRules: 1,
			expected: []rules.Rule{
				{
					ID:     "ecceadd1-d9ca-4ec9-a906-0e3e4736a45e",
					Domain: "example.com",
					IPs: []net.IPAddr{
						{IP: net.ParseIP("23.192.228.84")},
						{IP: net.ParseIP("23.215.0.138")},
					},
					Permanent:  true,
					Expires:    time.Time{},
					ResolvedAt: time.Time{},
				},
			},
		},
		{
			name: "one rule parsed, no expires, no ending newline",
			in: `# void-anchor
# Options
set block-policy drop
set fingerprints "/etc/pf.os"
set ruleset-optimization basic
set skip on lo0

# void ruleset for blocking sites
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e BEGIN ===
# Domain: example.com
block return out proto tcp from any to 23.192.228.84
block return out proto udp from any to 23.192.228.84
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e END ===`,
			expectedRules: 1,
			expected: []rules.Rule{
				{
					ID:     "ecceadd1-d9ca-4ec9-a906-0e3e4736a45e",
					Domain: "example.com",
					IPs: []net.IPAddr{
						{IP: net.ParseIP("23.192.228.84")},
					},
					Permanent:  true,
					Expires:    time.Time{},
					ResolvedAt: time.Time{},
				},
			},
		},
		{
			name: "one rule parsed, no expires, no ending newline",
			in: `# void-anchor
# Options
set block-policy drop
set fingerprints "/etc/pf.os"
set ruleset-optimization basic
set skip on lo0

# void ruleset for blocking sites
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e BEGIN ===
# Domain: example.com
block return out proto tcp from any to 23.192.228.84
block return out proto udp from any to 23.192.228.84
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e END ===`,
			expectedRules: 1,
			expected: []rules.Rule{
				{
					ID:     "ecceadd1-d9ca-4ec9-a906-0e3e4736a45e",
					Domain: "example.com",
					IPs: []net.IPAddr{
						{IP: net.ParseIP("23.192.228.84")},
					},
					Permanent:  true,
					Expires:    time.Time{},
					ResolvedAt: time.Time{},
				},
			},
		},
		{
			name: "two rules parsed",
			in: `# void-anchor
# Options
set block-policy drop
set fingerprints "/etc/pf.os"
set ruleset-optimization basic
set skip on lo0

# void ruleset for blocking sites
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e BEGIN ===
# Domain: example.com
block return out proto tcp from any to 23.192.228.84
block return out proto udp from any to 23.192.228.84
# === VOID-RULE ecceadd1-d9ca-4ec9-a906-0e3e4736a45e END ===
# === VOID-RULE 0xdeadbeef BEGIN ===
# Domain: x.com
block return out proto tcp from any to 1.2.3.4
block return out proto udp from any to 1.2.3.4
# === VOID-RULE 0xdeadbeef END ===`,
			expectedRules: 2,
			expected: []rules.Rule{
				{
					ID:     "ecceadd1-d9ca-4ec9-a906-0e3e4736a45e",
					Domain: "example.com",
					IPs: []net.IPAddr{
						{IP: net.ParseIP("23.192.228.84")},
					},
					Permanent:  true,
					Expires:    time.Time{},
					ResolvedAt: time.Time{},
				},
				{
					ID:     "0xdeadbeef",
					Domain: "x.com",
					IPs: []net.IPAddr{
						{IP: net.ParseIP("1.2.3.4")},
					},
					Permanent:  true,
					Expires:    time.Time{},
					ResolvedAt: time.Time{},
				},
			},
		},
	}

	for _, tt := range tests {
		mockFS := &mocks.MockOsFS{}
		mockFS.On("ReadFile", _pfAnchorPath).Return([]byte(tt.in), nil)

		s.Run(tt.name, func() {
			m := ManagerImpl{
				fs:  mockFS,
				cmd: noexec{},
			}
			out, err := m.CurrentRules()
			if tt.expectErr {
				s.Error(err)
				s.Equal(tt.err, err)
			} else {
				s.NoError(err)
				s.Len(out, tt.expectedRules)
				for i, rule := range out {
					s.Equal(tt.expected[i].ID, rule.ID)
					s.Equal(tt.expected[i].Domain, rule.Domain)
					assert.ElementsMatch(s.T(), tt.expected[i].IPs, rule.IPs)
					s.Equal(tt.expected[i].Expires, rule.Expires)
					s.Equal(tt.expected[i].Permanent, rule.Permanent)
					s.Equal(tt.expected[i].ResolvedAt, rule.ResolvedAt)
				}
			}
		})
	}
}

func (s *PFTestSuite) TestRunWalk() {
	tests := []struct {
		name      string
		in        string
		expectErr bool
		out       int
	}{
		{
			name: "one rule parsed",
			in: `# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D514 BEGIN ===
# Domain: example.com
block return out proto tcp from any to 1.3.3.7
block return out proto udp from any to 1.3.3.7
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D514 END ===
`,
			out: 1,
		},
		{
			name: "malformed rule, no result",
			in: `# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D514 BEGIN ===
# Domain: example.com
block return out proto tcp from any to 1.3.3.7
`,
			out:       0,
			expectErr: false,
		},
		{
			name: "4 rules",
			in: `# Options
set block-policy drop
set fingerprints "/etc/pf.os"
set ruleset-optimization basic
set skip on lo0

# void ruleset for blocking sites
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D514 BEGIN ===
# Domain: example.com
block return out proto tcp from any to 1.3.3.7
block return out proto udp from any to 1.3.3.7
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D514 END ===
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D512 BEGIN ===
# Domain: example.com
block return out proto tcp from any to 1.3.3.8
block return out proto udp from any to 1.3.3.8
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D512 END ===
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D511 BEGIN ===
# Domain: example.com
block return out proto tcp from any to 1.3.4.7
block return out proto udp from any to 1.5.3.7
# === VOID-RULE C06B2055-B1FF-4C98-B0C6-6967BD59D511 END ===
`,
			out: 3,
		},
		{
			name:      "orphan END",
			in:        "# === VOID-RULE dead-beef END ===\n",
			expectErr: true,
		},
		{
			name: "mismatched IDs",
			in: `# === VOID-RULE A BEGIN ===
# stuff
# === VOID-RULE B END ===
`,
			expectErr: true,
		},
		{
			name: "nested BEGIN",
			in: `# === VOID-RULE A BEGIN ===
# === VOID-RULE B BEGIN ===
# === VOID-RULE B END ===
# === VOID-RULE A END ===
`,
			expectErr: true,
		},
		{
			name: "duplicate IDs",
			in: `# === VOID-RULE A BEGIN ===
# === VOID-RULE A END ===
# === VOID-RULE A BEGIN ===
# === VOID-RULE A END ===
`,
			expectErr: true,
		},
		{
			name: "windows line endings",
			in:   "# === VOID-RULE A BEGIN ===\r\n# === VOID-RULE A END ===\r\n",
			out:  1,
		},
		{
			name: "only header, no rules",
			in: `# void-anchor v1
# Options
set skip on lo0
`,
			out: 0,
		},
	}
	s.Run("walk", func() {
		for _, test := range tests {
			s.Run(test.name, func() {
				r := strings.NewReader(test.in)
				m := ManagerImpl{}
				m.cmd = noexec{}
				out, err := m.walk(r)
				s.Equal(test.expectErr, err != nil, "expected error %v", test.expectErr)
				s.Len(out, test.out, "expected %d block", test.out)
			})
		}
	})
}

func TestRunPFTestSuite(t *testing.T) {
	suite.Run(t, new(PFTestSuite))
}

// mustParseTime parses the time string using the given layout,
// or panics if parsing fails. Useful for test setup.
func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		// Panic is acceptable here because if the time string literal
		// in the test is invalid, the test itself is broken and cannot run.
		panic("test setup failure: could not parse time literal '" + value + "': " + err.Error())
	}
	return t
}

type noexec struct{}

func (noexec) Run(_ context.Context, _ string, _ ...string) error {
	return nil
}
