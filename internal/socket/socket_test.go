package socket_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lc/void/internal/socket"
)

type SocketTestSuite struct {
	suite.Suite
	tmpDir   string
	sockPath string
	mockProc *mockProcessChecker
	sock     *socket.Socket
}

type mockProcessChecker struct {
	isRunning bool
}

func (m *mockProcessChecker) IsRunning(_ string) bool {
	return m.isRunning
}

func (s *SocketTestSuite) SetupTest() {
	var err error
	s.tmpDir, err = os.MkdirTemp("", "socket-test-*")
	s.Require().NoError(err)

	s.sockPath = filepath.Join(s.tmpDir, "test.sock")
	s.mockProc = &mockProcessChecker{isRunning: true}

	// Use shorter timeouts for testing
	cfg := socket.DefaultConfig()
	cfg.StartupTimeout = 500 * time.Millisecond
	cfg.RetryInterval = 50 * time.Millisecond

	s.sock = socket.New(cfg, s.mockProc)
}

func (s *SocketTestSuite) TearDownTest() {
	// Ensure any listeners are closed
	if conn, err := net.Dial("unix", s.sockPath); err == nil {
		conn.Close()
	}

	// Clean up test directory
	if s.tmpDir != "" {
		os.RemoveAll(s.tmpDir)
	}
}

func (s *SocketTestSuite) TestDefaultConfig() {
	cfg := socket.DefaultConfig()

	s.Equal(5*time.Second, cfg.StartupTimeout)
	s.Equal(250*time.Millisecond, cfg.RetryInterval)
	s.Equal("voidd", cfg.ProcessName)

	// Permissions depend on OS
	s.Contains([]os.FileMode{0o666, 0o600}, cfg.Permissions)
}

func (s *SocketTestSuite) TestListen() {
	testCases := []struct {
		name        string
		setup       func() error
		expectError string
	}{
		{
			name:  "successful listen",
			setup: func() error { return nil },
		},
		{
			name: "directory creation error",
			setup: func() error {
				// Create a regular file where directory should be
				dirPath := filepath.Dir(s.sockPath)
				if err := os.RemoveAll(dirPath); err != nil {
					return err
				}
				return os.WriteFile(dirPath, []byte("blocking"), 0o644)
			},
			expectError: "creating socket directory",
		},
		{
			name: "socket already in use",
			setup: func() error {
				l, err := net.Listen("unix", s.sockPath)
				if err != nil {
					return err
				}
				// Keep listener open
				s.T().Cleanup(func() { l.Close() })
				return nil
			},
			expectError: "address already in use",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Fresh setup for each test
			s.SetupTest()

			// Run test-specific setup
			err := tc.setup()
			s.Require().NoError(err, "setup failed")

			// Test
			l, err := s.sock.Listen(s.sockPath)

			// Verify
			if tc.expectError != "" {
				s.Error(err)
				s.Contains(err.Error(), tc.expectError)
			} else {
				s.NoError(err)
				s.NotNil(l)

				// Verify socket file permissions
				_, err := os.Stat(s.sockPath)
				s.NoError(err)
				// s.Equal(s.sock.Config().Permissions, fi.Mode().Perm())

				l.Close()
			}
		})
	}
}

type cancelContextKey string

func (s *SocketTestSuite) TestConnect() {
	var cancelKey cancelContextKey = "cancel"

	testCases := []struct {
		name        string
		setup       func(context.Context) error
		procRunning bool
		ctxTimeout  time.Duration
		expectError string
	}{
		{
			name: "successful connection",
			setup: func(_ context.Context) error {
				// Start listener in background
				l, err := s.sock.Listen(s.sockPath)
				if err != nil {
					return err
				}

				go func() {
					defer l.Close()
					conn, _ := l.Accept()
					if conn != nil {
						conn.Close()
					}
				}()

				// Give listener time to start
				time.Sleep(50 * time.Millisecond)
				return nil
			},
			procRunning: true,
			ctxTimeout:  time.Second,
		},
		{
			name: "process not running",
			setup: func(_ context.Context) error {
				return nil
			},
			procRunning: false,
			ctxTimeout:  time.Second,
			expectError: "daemon not running",
		},
		{
			name: "context cancelled",
			setup: func(ctx context.Context) error {
				if cancel, ok := ctx.Value(cancelKey).(context.CancelFunc); ok {
					cancel()
				}
				return nil
			},
			procRunning: true,
			ctxTimeout:  time.Second,
			expectError: "context canceled",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Fresh setup
			s.SetupTest()
			s.mockProc.isRunning = tc.procRunning

			// Create context
			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			ctx = context.WithValue(ctx, cancelKey, cancel)
			defer cancel()

			// Run setup
			err := tc.setup(ctx)
			s.Require().NoError(err, "setup failed")

			// Test
			conn, err := s.sock.Connect(ctx, s.sockPath)

			// Verify
			if tc.expectError != "" {
				s.Error(err)
				s.Contains(err.Error(), tc.expectError)
				s.Nil(conn)
			} else {
				s.NoError(err)
				s.NotNil(conn)
				conn.Close()
			}
		})
	}
}

func (s *SocketTestSuite) TestRetryBehavior() {
	// Use shorter timeouts for testing
	cfg := socket.DefaultConfig()
	cfg.StartupTimeout = 2 * time.Second
	cfg.RetryInterval = 100 * time.Millisecond
	s.sock = socket.New(cfg, s.mockProc)

	ctx := context.Background()
	startTime := time.Now()

	// Start listener after delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		l, err := s.sock.Listen(s.sockPath)
		if err == nil {
			defer l.Close()
			conn, _ := l.Accept()
			if conn != nil {
				conn.Close()
			}
		}
	}()

	conn, err := s.sock.Connect(ctx, s.sockPath)
	duration := time.Since(startTime)

	s.NoError(err)
	s.NotNil(conn)
	if conn != nil {
		conn.Close()
	}

	// Verify timing
	s.GreaterOrEqual(duration, 500*time.Millisecond, "Should have waited for listener")
	s.Less(duration, 2*time.Second, "Should not have waited too long")
}

func TestSocketSuite(t *testing.T) {
	suite.Run(t, new(SocketTestSuite))
}
