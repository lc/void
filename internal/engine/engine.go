// Package engine orchestrates the core logic of the Void daemon.
// It manages the rule lifecycle, interacts with DNS resolution and the
// firewall manager (pf), and handles periodic refresh and expiry tasks.
// All state modifications are serialized through a single goroutine
// to ensure thread safety.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/multierr"

	"github.com/lc/void/internal/dnsresolver"
	"github.com/lc/void/internal/log"
	"github.com/lc/void/internal/pf"
	"github.com/lc/void/internal/rules"
)

const (
	// How often to check for expired rules and potentially refresh DNS.
	// A shorter interval ensures timely expiry but increases load.
	_tickerInterval = 30 * time.Second
	// Small buffer for commands to avoid blocking senders momentarily.
	_commandBufferSize = 10
)

// Engine orchestrates rule management, DNS resolution, and PF synchronization.
type Engine struct {
	store      rules.Store
	pfMgr      pf.Manager
	resolver   dnsresolver.Clienter
	dnsRefresh time.Duration // How often rule's DNS should be refreshed/re-resolved.

	cmdChan  chan command // Commands are processed serially by runLoop
	wg       sync.WaitGroup
	cancelFn context.CancelFunc // Cancels the context passed to Run
}

// New creates a new Engine instance.
// dnsRefreshInterval specifies how often DNS records for existing rules should be re-resolved.
func New(pfMgr pf.Manager, resolver dnsresolver.Clienter, dnsRefreshInterval time.Duration) *Engine {
	return &Engine{
		store:      rules.NewStore(),
		pfMgr:      pfMgr,
		resolver:   resolver,
		dnsRefresh: dnsRefreshInterval,
		cmdChan:    make(chan command, _commandBufferSize),
	}
}

// Run starts the engine's background processing goroutines.
// It loads initial rules from PF and starts the ticker and command loop.
// The provided context controls the lifetime of these goroutines.
func (e *Engine) Run(ctx context.Context) {
	// Create an internal context that we can cancel on Close()
	runCtx, cancel := context.WithCancel(ctx)
	e.cancelFn = cancel

	// Load initial state from PF before starting loops
	if err := e.loadInitialRules(runCtx); err != nil {
		log.Warnf("engine: failed to load initial rules: %v", err)
	}

	e.wg.Add(2)
	go e.runLoop(runCtx)
	go e.runTicker(runCtx)

	log.Info("engine: started")
}

// Close gracefully shuts down the engine's background goroutines.
func (e *Engine) Close() {
	if e.cancelFn != nil {
		e.cancelFn() // Signal goroutines to stop
	}
	e.wg.Wait()
	log.Info("engine: stopped")
}

// BlockDomain initiates the process of blocking a domain.
// It resolves the domain to IPs and adds a rule to the store.
// The actual PF update happens asynchronously within the engine's runLoop.
func (e *Engine) BlockDomain(ctx context.Context, domain string, ttl time.Duration) error {
	cmd := blockCmd{
		domain: domain,
		ttl:    ttl,
	}

	select {
	case e.cmdChan <- cmd:
		return nil // Command successfully queued
	case <-ctx.Done():
		return ctx.Err() // Request context cancelled
	}
}

// UnblockDomain initiates the removal of a rule by its ID.
// The actual PF update happens asynchronously within the engine's runLoop.
func (e *Engine) UnblockDomain(id string) {
	// Use a background context for sending the command, as the API handler
	// might return before the command is processed. The engine's lifecycle
	// context (runCtx) ensures processing stops eventually on shutdown.
	ctx := context.Background()

	cmd := unblockCmd{id: id}

	select {
	case e.cmdChan <- cmd:
		// Command queued
	case <-ctx.Done(): // unlikely, but check
		log.Warnf("engine: failed to queue unblock command for %s: %v", id, ctx.Err())
	}
}

// Snapshot returns a read-only copy of the current rules managed by the engine.
func (e *Engine) Snapshot() []rules.Rule {
	return e.store.Snapshot()
}

// runLoop is the central processing loop. It serializes all state changes.
func (e *Engine) runLoop(ctx context.Context) {
	defer e.wg.Done()
	defer func() {
		// Drain command channel on exit? Maybe not necessary if Close waits.
		log.Warnf("engine: runLoop stopping")
	}()

	log.Info("engine: runLoop starting")

	for {
		select {
		case cmd := <-e.cmdChan:
			// Process commands serially
			needsSync := false
			var err error
			switch c := cmd.(type) {
			case blockCmd:
				needsSync, err = e.handleBlock(ctx, c)
				if err != nil {
					log.Warnf("engine: error handling block command for %q: %v", c.domain, err)
					// TODO: Propagate error back via c.errChan if added
				}
			case unblockCmd:
				needsSync, err = e.handleUnblock(ctx, c)
				if err != nil {
					log.Warnf("engine: error handling unblock command for %q: %v", c.id, err)
					// TODO: Propagate error back via c.errChan if added
				}
			case refreshExpireCmd:
				needsSync, err = e.handleRefreshExpire(ctx)
				if err != nil {
					log.Warnf("engine: error handling refresh/expire: %v", err)
				}
			default:
				log.Warnf("engine: received unknown command type: %T", cmd)
			}

			// Sync PF if any command resulted in a state change
			if needsSync {
				if syncErr := e.syncPF(ctx); syncErr != nil {
					log.Infof("engine: failed to sync pf: %v", syncErr)
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// runTicker periodically sends refresh/expire commands to the runLoop.
func (e *Engine) runTicker(ctx context.Context) {
	defer e.wg.Done()
	defer log.Info("engine: runTicker stopping")

	log.Info("engine: runTicker starting")
	ticker := time.NewTicker(_tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			select {
			case e.cmdChan <- refreshExpireCmd{}:
			case <-ctx.Done():
				return
			default:
				// Should not happen with a buffered channel unless severely backed up
				log.Info("engine: warning: command channel full, skipping ticker cycle")
			}
		case <-ctx.Done():
			return
		}
	}
}

// --- Command Handlers (run only within runLoop) ---
func (e *Engine) handleBlock(ctx context.Context, cmd blockCmd) (needsSync bool, err error) {
	log.Infof("engine: handling block request for %q (ttl: %v)", cmd.domain, cmd.ttl)

	ips, err := e.resolver.LookupHost(ctx, cmd.domain)
	if err != nil {
		// Don't block if DNS fails initially, maybe log? Or should we error?
		// For now, let's log and not proceed with adding the rule.
		return false, fmt.Errorf("dns lookup failed for %q: %w", cmd.domain, err)
		// Alternatively, create a rule with no IPs and let refresh handle it?
	}
	if len(ips) == 0 {
		// Should be covered by dnsresolver error, but check defensively.
		return false, fmt.Errorf("no IPs resolved for %q", cmd.domain)
	}

	now := time.Now()
	rule := &rules.Rule{
		ID:         uuid.NewString(), // Generate a new unique ID
		Domain:     cmd.domain,
		IPs:        ips,
		ResolvedAt: now,
	}

	if cmd.ttl > 0 {
		rule.Expires = now.Add(cmd.ttl)
		rule.Permanent = false
	} else {
		rule.Permanent = true
	}

	changed := e.store.Upsert(rule)
	if changed {
		log.Infof("engine: added/updated rule ID %s for domain %s", rule.ID, rule.Domain)
	} else {
		log.Infof("engine: block request for existing permanent domain %s ignored", rule.Domain)
	}

	return changed, nil
}

func (e *Engine) handleUnblock(_ context.Context, cmd unblockCmd) (needsSync bool, err error) {
	log.Infof("engine: handling unblock request for ID %q", cmd.id)
	removedRule, found := e.store.Remove(cmd.id)
	if found {
		log.Infof("engine: removed rule ID %s for domain %s", removedRule.ID, removedRule.Domain)
		return true, nil // Need to sync PF
	}
	log.Infof("engine: unblock request for non-existent ID %q ignored", cmd.id)
	return false, nil
}

func (e *Engine) handleRefreshExpire(ctx context.Context) (needsSync bool, err error) {
	log.Debug("engine: handling refresh/expire cycle")
	now := time.Now()
	var changed bool

	// 1. Expire rules
	expired := e.store.ExpireNow(now)
	if len(expired) > 0 {
		changed = true
		for _, r := range expired {
			log.Infof("engine: expired rule ID %s for domain %s", r.ID, r.Domain)
		}
	}

	// 2. Refresh DNS for existing rules nearing refresh time
	var refreshErrors error
	for _, rule := range e.store.Snapshot() { // Get a copy to iterate over
		// Check if rule needs refresh (e.g., older than 90% of refresh interval)
		if rule.ResolvedAt.IsZero() || time.Since(rule.ResolvedAt) > (e.dnsRefresh*9/10) {
			log.Infof("engine: refreshing DNS for rule ID %s (%s)", rule.ID, rule.Domain)
			newIPs, err := e.resolver.LookupHost(ctx, rule.Domain)
			if err != nil {
				refreshErrors = multierr.Append(refreshErrors, fmt.Errorf("refresh failed for %s (%s): %w", rule.ID, rule.Domain, err))
				continue
			}

			// Check if IPs actually changed
			if !ipsEqual(rule.IPs, newIPs) {
				log.Infof("engine: IPs changed for rule ID %s (%s)", rule.ID, rule.Domain)
				// Create updated rule object (keep ID, Expires, Permanent)
				updatedRule := &rules.Rule{
					ID:         rule.ID,
					Domain:     rule.Domain,
					IPs:        newIPs,
					Expires:    rule.Expires, // Keep original expiry
					Permanent:  rule.Permanent,
					ResolvedAt: now, // Update resolution time
				}
				// Upsert should handle replacing the existing entry by ID
				if e.store.Upsert(updatedRule) {
					changed = true // IPs changed, need sync
				}
			} else {
				log.Debugf("engine: IPs unchanged for rule ID %s (%s), updating resolution time", rule.ID, rule.Domain)
				if e.store.UpdateResolvedAt(rule.ID, now) {
					changed = true // ResolvedAt changed, need sync
				}
			}
		}
	}

	return changed, refreshErrors // Return collected errors
}

// syncPF pushes the current ruleset state to the firewall manager.
func (e *Engine) syncPF(ctx context.Context) error {
	log.Info("engine: synchronizing rules with PF")
	currentRules := e.store.Snapshot()
	// Pass the engine's run context, not the original request context
	err := e.pfMgr.Sync(ctx, currentRules)
	if err != nil {
		return fmt.Errorf("pfMgr.Sync failed: %w", err)
	}
	log.Info("engine: PF synchronization successful")
	return nil
}

// loadInitialRules reads the current rules from PF on startup.
// This prevents overwriting existing rules managed by a previous daemon instance.
func (e *Engine) loadInitialRules(_ context.Context) error {
	log.Info("engine: loading initial rules from PF")
	initialRules, err := e.pfMgr.CurrentRules()
	if err != nil {
		// If file doesn't exist, that's okay (first run). Other errors are problematic.
		if errors.Is(err, fs.ErrNotExist) {
			log.Info("engine: no existing PF anchor file found, starting fresh.")
			return nil
		}
		return fmt.Errorf("pfMgr.CurrentRules failed: %w", err)
	}

	if len(initialRules) == 0 {
		log.Info("engine: existing PF anchor file is empty.")
		return nil
	}

	loadedCount := 0
	for _, r := range initialRules {
		// Need to make a copy because we pass a pointer to Upsert
		ruleToLoad := r
		// Set ResolvedAt to now if missing, otherwise refresh logic might trigger immediately.
		// Or maybe load it as needing immediate refresh? Let's set to now.
		if ruleToLoad.ResolvedAt.IsZero() {
			ruleToLoad.ResolvedAt = time.Now()
		}
		// Upsert silently adds the rules to the store without triggering a PF sync yet.
		if e.store.Upsert(&ruleToLoad) {
			loadedCount++
		}
	}

	log.Infof("engine: loaded %d initial rules from PF", loadedCount)
	return nil
}

// command interface defines the structure of commands sent to the engine.
type command interface {
	isCommand()
}

type blockCmd struct {
	domain string
	ttl    time.Duration
}

func (blockCmd) isCommand() {}

type unblockCmd struct {
	id string
}

func (unblockCmd) isCommand() {}

type refreshExpireCmd struct{}

func (refreshExpireCmd) isCommand() {}

// ipsEqual checks if two slices of net.IPAddr contain the same IPs, ignoring order.
func ipsEqual(a, b []net.IPAddr) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}

	counts := make(map[string]int)

	for _, ip := range a {
		key := ipKey(ip)
		counts[key]++
	}
	for _, ip := range b {
		key := ipKey(ip)
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}

func ipKey(ip net.IPAddr) string {
	return ip.IP.String() + "%" + ip.Zone
}
