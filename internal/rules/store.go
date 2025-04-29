// Package rules provides domain rule management for the Void domain blocking system.
// It defines the Rule type and Store interface for managing rules in memory,
// with support for rule expiration, DNS resolution tracking, and atomic operations.
package rules

import (
	"container/heap"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"
)

// Rule represents a single domain blocking rule in the PF ruleset.
// Each rule contains information about a domain to block, its associated
// IP addresses, and metadata about expiration and resolution times.
type Rule struct {
	ID         string       // Unique identifier for the rule
	Domain     string       // Domain name to block
	IPs        []net.IPAddr // Resolved IP addresses for the domain
	Expires    time.Time    // When the rule expires (zero for permanent rules)
	Permanent  bool         // Whether the rule is permanent
	ResolvedAt time.Time    // When the domain was last resolved to IPs
}

var _ Store = (*MemoryStore)(nil)

type Store interface {
	// Upsert inserts or updates a rule. Returns true if PF needs to be updated.
	Upsert(r *Rule) (changed bool)
	// UpdateResolvedAt updates the ResolvedAt timestamp for a rule.
	UpdateResolvedAt(id string, ts time.Time) bool
	// Remove deletes and returns the rule for logging/PF diff.
	Remove(id string) (*Rule, bool)
	// NextRefresh returns the earliest time any rule needs a DNS refresh.
	NextRefresh(rr time.Duration) time.Time
	// NextExpiry returns the soonest expiry time, or ok=false if none.
	NextExpiry() (time.Time, bool)
	// ExpireNow pops all entries older than now.
	ExpireNow(now time.Time) []*Rule
	// Snapshot returns a copy of the current ruleset.
	Snapshot() []Rule
}

// NewStore creates a new in-memory rule store.
// The returned store is thread-safe and ready to use.
func NewStore() *MemoryStore {
	return &MemoryStore{
		byID:  make(map[string]*entry),
		byDom: make(map[string]*entry),
		expH:  make([]*entry, 0),
	}
}

// MemoryStore is an in-memory implementation of the Store interface.
// It provides thread-safe operations for managing rules with efficient
// lookups by ID and domain, as well as expiration tracking.
type MemoryStore struct {
	mu    sync.RWMutex      // protects fields below
	byID  map[string]*entry // id -> entry
	byDom map[string]*entry // lower-cased FQDN -> entry
	expH  expiryHeap        // min-heap keyed by .Expires
	count atomic.Int64      // metrics: total rules
}

// Upsert inserts or upgrades a rule. Returns true if PF needs to be updated:
func (s *MemoryStore) Upsert(r *Rule) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dom := strings.ToLower(r.Domain)

	if cur, ok := s.byDom[dom]; ok {
		// If the existing rule is temporary & new is permanent, upgrade in place.
		if !cur.Permanent && r.Permanent {
			cur.Permanent = true
			cur.Expires = time.Time{}
			// permanent rules don't exist in the heap.
			heap.Remove(&s.expH, cur.heapIdx)
			return true
		}
		// otherwise, update the existing rule.
		cur.IPs = r.IPs
		cur.ResolvedAt = r.ResolvedAt
		cur.Expires = r.Expires
		return true
	}

	e := &entry{Rule: r}
	s.byID[r.ID] = e
	s.byDom[dom] = e

	// only push temp rules to heap.
	if !r.Permanent {
		heap.Push(&s.expH, e)
	}
	s.count.Inc()
	return true
}

// UpdateResolvedAt updates the ResolvedAt timestamp for a rule.
func (s *MemoryStore) UpdateResolvedAt(id string, ts time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.byID[id]
	if !ok {
		return false
	}
	cur.ResolvedAt = ts

	// Update the heap if this is a temporary rule.
	if !cur.Permanent {
		heap.Fix(&s.expH, cur.heapIdx)
	}

	cur, ok = s.byDom[cur.Domain]
	if !ok {
		return false
	}

	cur.ResolvedAt = ts
	return true
}

// Remove deletes by id; returns the rule for logging/PF diff.
func (s *MemoryStore) Remove(id string) (*Rule, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.byID[id]
	if !ok {
		return nil, false
	}

	delete(s.byID, cur.ID)
	delete(s.byDom, strings.ToLower(cur.Domain))

	if !cur.Permanent {
		heap.Remove(&s.expH, cur.heapIdx)
	}

	s.count.Dec()
	return cur.Rule, true
}

// NextRefresh returns the earliest time any rule needs a DNS refresh.
func (s *MemoryStore) NextRefresh(rr time.Duration) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	soonest := time.Now().Add(rr) // upper bound
	for _, e := range s.byID {
		ts := e.ResolvedAt.Add(rr)
		if ts.Before(soonest) {
			soonest = ts
		}
	}
	return soonest
}

// NextExpiry returns the soonest expiry time, or ok=false if none.
func (s *MemoryStore) NextExpiry() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.expH) == 0 {
		return time.Time{}, false
	}
	return s.expH[0].Expires, true
}

// ExpireNow pops all entries older than now.
func (s *MemoryStore) ExpireNow(now time.Time) []*Rule {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []*Rule
	// Keep popping while we have entries and the top is expired
	for s.expH.Len() > 0 {
		if s.expH[0].Expires.After(now) {
			break // No more expired entries
		}
		popped := heap.Pop(&s.expH)
		e, ok := popped.(*entry)
		if !ok {
			// This should never happen, but handle it gracefully
			continue
		}
		delete(s.byID, e.ID)
		delete(s.byDom, strings.ToLower(e.Domain))
		s.count.Dec()
		expired = append(expired, e.Rule)
	}

	return expired
}

// Snapshot returns a copy of the current ruleset.
func (s *MemoryStore) Snapshot() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]Rule, 0, len(s.byID))
	for _, e := range s.byID {
		rules = append(rules, *e.Rule) // value copy
	}
	return rules
}

// entry is what we keep internally (pointer for in-place updates).
type entry struct {
	*Rule
	// index inside expiryHeap for O(log n) removal/update.
	heapIdx int
}

// expiryHeap is a min-heap ordered by Rule.Expires.
// Implementation notes
//   - Satisfies container/heap.Interface so callers must use heap.Push/Pop.
//   - Len/Less/Swap have value receivers because they don’t mutate the slice
//     header—this matches Kubernetes’ waitForPriorityQueue pattern.
//   - Push/Pop mutate the slice header and therefore use pointer receivers.
//
// Concurrency: the heap is *not* thread-safe. All access is protected by
// Index.mu. Do NOT touch it without holding the lock.
type expiryHeap []*entry

var _ heap.Interface = (*expiryHeap)(nil)

// -------- read-only methods --------

// Len returns the number of elements in the heap.
// Value receiver: cheap header copy; no mutation.
func (h expiryHeap) Len() int { return len(h) }

// Less reports whether element i should sort before j (min-heap).
// Value receiver: no header mutation. Either element may be the zero
// time, but permanent rules should never appear here by design.
func (h expiryHeap) Less(i, j int) bool {
	return h[i].Expires.Before(h[j].Expires)
}

// Swap exchanges elements i and j.
// Value receiver OK: we mutate underlying array, not the header.
func (h expiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIdx, h[j].heapIdx = i, j
}

// -------- mutating methods --------

// Push inserts x into the heap (container/heap calls this).
// Pointer receiver required: we append to the slice.
func (h *expiryHeap) Push(x any) {
	e, ok := x.(*entry)
	if !ok {
		// This should never happen in practice since we control all callers,
		// but handle it gracefully to satisfy the linter
		return
	}
	e.heapIdx = len(*h)
	*h = append(*h, e)
}

// Pop removes and returns the minimum element (container/heap calls this).
// Pointer receiver required: slice header shrinks.
func (h *expiryHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	e.heapIdx = -1 // Mark as no longer in heap
	*h = old[:n-1]
	return e
}
