// Package socket provides Unix domain socket functionality for void daemon communication.
package socket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	// ErrAddressInUse is returned when attempting to listen on a socket that is already in use.
	ErrAddressInUse = errors.New("address already in use")
	// ErrNotRunning is returned when the daemon process is not running.
	ErrNotRunning = errors.New("daemon not running")
)

// Config holds socket configuration options for the Socket.
// It controls timeout behavior, retry intervals, file permissions,
// and process name for daemon detection.
type Config struct {
	// StartupTimeout is the maximum time to wait for daemon startup
	StartupTimeout time.Duration
	// RetryInterval is the interval between connection attempts
	RetryInterval time.Duration
	// Permissions defines the socket file permissions
	Permissions os.FileMode
	// ProcessName is the name of the daemon process to look for
	ProcessName string
}

// DefaultConfig returns a new Config with sensible default values.
// This includes a 5-second startup timeout, 250ms retry interval,
// OS-appropriate socket permissions, and "voidd" as the process name.
func DefaultConfig() *Config {
	return &Config{
		StartupTimeout: 5 * time.Second,
		RetryInterval:  250 * time.Millisecond,
		Permissions:    getDefaultPermissions(),
		ProcessName:    "voidd",
	}
}

// Socket manages Unix domain socket operations for the Void daemon.
// It provides methods for connecting to and listening on Unix domain sockets,
// with support for retrying connections, checking if the daemon process is running,
// and handling socket permissions and directory creation.
type Socket struct {
	config    *Config
	procCheck ProcessChecker
	startTime time.Time
	mu        sync.RWMutex
}

// New creates a new Socket with the given configuration and process checker.
// If cfg is nil, DefaultConfig() will be used.
// The returned Socket is ready to use for connection or listening operations.
func New(cfg *Config, checker ProcessChecker) *Socket {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Socket{
		config:    cfg,
		procCheck: checker,
		startTime: time.Now(),
	}
}

// ConnectContext establishes a connection to the daemon socket with context support.
// It uses default configuration and process checker.
// The context can be used to cancel the connection attempt.
func ConnectContext(ctx context.Context, path string) (net.Conn, error) {
	s := New(nil, &DefaultProcessChecker{})
	return s.Connect(ctx, path)
}

// Listen creates a Unix domain socket listener at the specified path.
// It uses default configuration and process checker.
func Listen(path string) (net.Listener, error) {
	s := New(nil, &DefaultProcessChecker{})
	return s.Listen(path)
}

// Connect establishes a connection to the daemon socket with context support.
// It will retry connecting until the context is canceled, the startup timeout is reached,
// or a successful connection is established. If the daemon is not running after
// the timeout period, ErrNotRunning is returned.
func (s *Socket) Connect(ctx context.Context, path string) (net.Conn, error) {
	deadline := time.Now().Add(s.config.StartupTimeout)

	for {
		conn, err := s.tryConnect(ctx, path)
		if err == nil {
			return conn, nil
		}

		if !s.shouldRetry(deadline) {
			return nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.config.RetryInterval):
			continue
		}
	}
}

// Listen creates a Unix domain socket listener at the specified path.
// It ensures the socket directory exists, removes any stale socket file,
// and sets appropriate permissions on the socket. If the socket is already
// in use, ErrAddressInUse is returned.
func (s *Socket) Listen(path string) (net.Listener, error) {
	if err := s.ensureSocketDirectory(path); err != nil {
		return nil, err
	}

	if err := s.checkExistingSocket(path); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("creating socket listener: %w", err)
	}

	if err := os.Chmod(path, s.config.Permissions); err != nil {
		listener.Close()
		return nil, fmt.Errorf("setting socket permissions: %w", err)
	}

	return listener, nil
}

func (s *Socket) tryConnect(ctx context.Context, path string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, "unix", path)
}

func (s *Socket) shouldRetry(deadline time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if time.Now().After(deadline) {
		return false
	}

	// Quick check for early startup
	if time.Since(s.startTime) < 2*time.Second {
		return true
	}

	return s.procCheck.IsRunning(s.config.ProcessName)
}

// ensureSocketDirectory ensures the socket directory exists and has the correct permissions.
func (s *Socket) ensureSocketDirectory(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating socket directory: %w", err)
	}

	// Adjust directory permissions if needed
	if s.config.Permissions == 0o666 {
		if fi, err := os.Stat(dir); err == nil && fi.Mode()&0o077 == 0 {
			if err := os.Chmod(dir, 0o755); err != nil {
				return fmt.Errorf("setting directory permissions: %w", err)
			}
		}
	}

	return nil
}

// checkExistingSocket checks if a socket already exists at the given path.
func (s *Socket) checkExistingSocket(path string) error {
	conn, err := net.Dial("unix", path)
	if err == nil {
		_ = conn.Close()
		return ErrAddressInUse
	}

	// Remove stale socket file
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale socket: %w", err)
	}

	return nil
}

// getDefaultPermissions returns the default socket permissions based on OS.
func getDefaultPermissions() os.FileMode {
	if usesPeerCreds() {
		return 0o666
	}
	return 0o600
}

// usesPeerCreds reports whether the current OS supports peer credentials.
func usesPeerCreds() bool {
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd":
		return true
	default:
		return false
	}
}
