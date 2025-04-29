package rules

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type StoreTestSuite struct {
	suite.Suite
	store *MemoryStore
}

func (s *StoreTestSuite) SetupTest() {
	s.store = NewStore()
}

func (s *StoreTestSuite) TestUpsert() {
	testCases := []struct {
		name          string
		initialRules  []*Rule
		ruleToAdd     *Rule
		expectChanged bool
		expectCount   int64
		expectExists  bool
	}{
		{
			name: "add new temporary rule",
			ruleToAdd: &Rule{
				ID:      "test1",
				Domain:  "example.com",
				IPs:     []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}},
				Expires: time.Now().Add(time.Hour),
			},
			expectChanged: true,
			expectCount:   1,
			expectExists:  true,
		},
		{
			name: "add new permanent rule",
			ruleToAdd: &Rule{
				ID:        "test2",
				Domain:    "example.org",
				IPs:       []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}},
				Permanent: true,
			},
			expectChanged: true,
			expectCount:   1,
			expectExists:  true,
		},
		{
			name: "upgrade temporary to permanent",
			initialRules: []*Rule{
				{
					ID:      "test3",
					Domain:  "example.net",
					IPs:     []net.IPAddr{{IP: net.ParseIP("9.10.11.12")}},
					Expires: time.Now().Add(time.Hour),
				},
			},
			ruleToAdd: &Rule{
				ID:        "test3",
				Domain:    "example.net",
				IPs:       []net.IPAddr{{IP: net.ParseIP("9.10.11.12")}},
				Permanent: true,
			},
			expectChanged: true,
			expectCount:   1,
			expectExists:  true,
		},
		{
			name: "duplicate temporary rule",
			initialRules: []*Rule{
				{
					ID:      "test4",
					Domain:  "example.com",
					IPs:     []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}},
					Expires: time.Now().Add(time.Hour),
				},
			},
			ruleToAdd: &Rule{
				ID:      "test5",
				Domain:  "example.com",
				IPs:     []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}},
				Expires: time.Now().Add(2 * time.Hour),
			},
			expectChanged: true,
			expectCount:   1,
			expectExists:  false, // Should not exist as it's a duplicate
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest() // Reset store for each test case

			// Add initial rules if any
			for _, r := range tc.initialRules {
				s.store.Upsert(r)
			}

			// Perform the test
			changed := s.store.Upsert(tc.ruleToAdd)

			// Verify expectations
			s.Equal(tc.expectChanged, changed)
			s.Equal(tc.expectCount, s.store.count.Load())

			// Verify internal state
			s.store.mu.RLock()
			defer s.store.mu.RUnlock()

			// Check maps
			entry, exists := s.store.byID[tc.ruleToAdd.ID]
			s.Equal(tc.expectExists, exists)

			if tc.expectExists {
				s.Equal(tc.ruleToAdd.Domain, entry.Domain)
				s.Equal(tc.ruleToAdd.Permanent, entry.Permanent)

				// Check heap for temporary rules
				if !tc.ruleToAdd.Permanent {
					s.Greater(len(s.store.expH), 0)
					s.Equal(entry.heapIdx, s.store.expH[entry.heapIdx].heapIdx)
				}
			}
		})
	}
}

func (s *StoreTestSuite) TestExpiry() {
	// Use a fixed reference time to avoid timing issues
	baseTime := time.Now()

	testCases := []struct {
		name          string
		rules         []*Rule
		checkTime     time.Time
		expectExpired int
		expectNext    *time.Time
	}{
		{
			name:      "no rules",
			checkTime: baseTime,
		},
		{
			name: "single future expiry",
			rules: []*Rule{
				{
					ID:      "test1",
					Domain:  "example.com",
					Expires: baseTime.Add(time.Hour),
				},
			},
			checkTime:  baseTime,
			expectNext: timePtr(baseTime.Add(time.Hour)),
		},
		{
			name: "multiple expired rules",
			rules: []*Rule{
				{
					ID:      "test1",
					Domain:  "example1.com",
					Expires: baseTime.Add(-time.Hour),
				},
				{
					ID:      "test2",
					Domain:  "example2.com",
					Expires: baseTime.Add(-30 * time.Minute),
				},
			},
			checkTime:     baseTime,
			expectExpired: 2,
			expectNext:    nil, // No next expiry as all rules expire
		},
		{
			name: "mix of expired and future rules",
			rules: []*Rule{
				{
					ID:      "test1",
					Domain:  "example1.com",
					Expires: baseTime.Add(-time.Hour),
				},
				{
					ID:      "test2",
					Domain:  "example2.com",
					Expires: baseTime.Add(time.Hour),
				},
			},
			checkTime:     baseTime,
			expectExpired: 1,
			expectNext:    timePtr(baseTime.Add(time.Hour)),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()

			// Add test rules
			for _, r := range tc.rules {
				s.store.Upsert(r)
			}

			// Check expired rules
			expired := s.store.ExpireNow(tc.checkTime)
			s.Len(expired, tc.expectExpired, "should expire correct number of rules")

			// Check next expiry
			next, ok := s.store.NextExpiry()
			if tc.expectNext == nil {
				s.False(ok, "should not have next expiry")
			} else {
				s.True(ok, "should have next expiry")
				s.Equal(tc.expectNext.Unix(), next.Unix(), "next expiry time should match")
			}

			// Verify remaining rules count matches non-expired rules
			remainingCount := len(tc.rules) - tc.expectExpired
			s.store.mu.RLock()
			s.Len(s.store.expH, remainingCount, "heap should contain only non-expired rules")
			s.Equal(int64(remainingCount), s.store.count.Load(), "count should match remaining rules")
			s.store.mu.RUnlock()
		})
	}
}

func (s *StoreTestSuite) TestRemove() {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	testCases := []struct {
		name         string
		initialRule  *Rule
		ruleToRemove *Rule
		expectFound  bool
	}{
		{
			name: "remove existing temporary rule",
			initialRule: &Rule{
				ID:      "test1",
				Domain:  "example.com",
				Expires: baseTime.Add(time.Hour),
			},
			ruleToRemove: &Rule{ID: "test1"},
			expectFound:  true,
		},
		{
			name: "remove existing permanent rule",
			initialRule: &Rule{
				ID:        "test2",
				Domain:    "example.org",
				Permanent: true,
			},
			ruleToRemove: &Rule{ID: "test2"},
			expectFound:  true,
		},
		{
			name:         "remove non-existent rule",
			ruleToRemove: &Rule{ID: "nonexistent"},
			expectFound:  false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()

			// Add initial rule if any
			if tc.initialRule != nil {
				s.store.Upsert(tc.initialRule)
			}

			// Perform removal
			removed, found := s.store.Remove(tc.ruleToRemove.ID)

			// Verify expectations
			s.Equal(tc.expectFound, found)

			if tc.expectFound {
				s.NotNil(removed, "removed rule should not be nil")
				s.Equal(tc.initialRule.ID, removed.ID)
				s.Equal(tc.initialRule.Domain, removed.Domain)

				// Verify rule was actually removed
				s.store.mu.RLock()
				_, existsID := s.store.byID[tc.ruleToRemove.ID]
				s.False(existsID, "rule should not exist in byID after removal")

				_, existsDomain := s.store.byDom[strings.ToLower(tc.initialRule.Domain)]
				s.False(existsDomain, "rule should not exist in byDom after removal")

				// For temporary rules, verify heap state
				if !tc.initialRule.Permanent {
					for _, e := range s.store.expH {
						s.NotEqual(tc.initialRule.ID, e.ID, "rule should not exist in heap")
					}
				}
				s.store.mu.RUnlock()

				s.Equal(int64(0), s.store.count.Load(), "count should be zero after removal")
			} else {
				s.Nil(removed, "removed rule should be nil for non-existent rule")
			}
		})
	}
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreTestSuite))
}
