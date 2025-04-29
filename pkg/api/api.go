// Package api exposes a tiny JSON‑over‑HTTP API for the Void daemon.
// It listens on a Unix domain socket (path comes from config) and delegates
// all business logic to internal/engine.Engine.  No third‑party HTTP
// framework is used—just net/http + encoding/json—keeping the binary small
// and dependency‑free, which matches Uber’s "start minimal" guidance.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lc/void/internal/buildinfo"
	"github.com/lc/void/internal/engine"
	"github.com/lc/void/internal/socket"
)

// BlockRequest represents a request to block a domain.
type BlockRequest struct {
	Domain string        `json:"domain"`
	TTL    time.Duration `json:"ttl,omitempty"` // 0 = permanent
}

// BlockResponse represents a response to a block request.
type BlockResponse struct {
	ID string `json:"id"`
}

// UnblockRequest represents a request to unblock a domain.
type UnblockRequest struct {
	ID string `json:"id"`
}

// StatusResponse represents the server status response.
type StatusResponse struct {
	Rules   int           `json:"rules"`
	Uptime  time.Duration `json:"uptime"`
	Version string        `json:"version"`
	Commit  string        `json:"commit"`
}

// -------- server -----------------------------------------------------

// Server handles HTTP API requests over a Unix domain socket.
type Server struct {
	eng   *engine.Engine
	start time.Time
	mux   *http.ServeMux
	srv   *http.Server
}

// New creates a new API server with the given engine.
// It sets up the HTTP routes and returns a server ready to listen.
func New(eng *engine.Engine) *Server {
	s := &Server{
		eng:   eng,
		start: time.Now(),
		mux:   http.NewServeMux(),
	}

	s.mux.HandleFunc("/v1/block", s.handleBlock)
	s.mux.HandleFunc("/v1/unblock", s.handleUnblock)
	s.mux.HandleFunc("/v1/status", s.handleStatus)
	s.mux.HandleFunc("/v1/rules", s.handleRules)

	s.srv = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// ListenAndServe starts the Unix‑socket HTTP server.
func (s *Server) ListenAndServe(path string) error {
	ln, err := socket.Listen(path)
	if err != nil {
		return err
	}
	return s.srv.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }

// handleBlock adds a domain to the ruleset.
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req BlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Domain == "" {
		http.Error(w, "domain required", http.StatusBadRequest)
		return
	}
	if err := s.eng.BlockDomain(r.Context(), req.Domain, req.TTL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// ID is generated inside BlockDomain; we don’t have it here-return 204.
	w.WriteHeader(http.StatusNoContent)
}

// handleUnblock removes a domain from the ruleset.
func (s *Server) handleUnblock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req UnblockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	s.eng.UnblockDomain(req.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleStatus returns the server status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := StatusResponse{
		Rules:   len(s.eng.Snapshot()),
		Uptime:  time.Since(s.start),
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleRules returns the current ruleset.
func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := json.NewEncoder(w).Encode(s.eng.Snapshot()); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}
}
