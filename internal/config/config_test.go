package config_test

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lc/void/internal/config"
)

type ConfigTestSuite struct {
	suite.Suite
	fs       mockFS
	provider config.Provider
}

type mockFS struct {
	files map[string]string
}

func (m mockFS) Stat(path string) (os.FileInfo, error) {
	if _, ok := m.files[path]; !ok {
		return nil, os.ErrNotExist
	}
	return nil, nil
}

func (m mockFS) MkdirAll(_ string, _ os.FileMode) error {
	return nil
}

func (m mockFS) Open(path string) (*os.File, error) {
	content, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	tmp, err := os.CreateTemp("", "mock-*") // caller cleans up in t.Cleanup
	if err != nil {
		return nil, err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return nil, err
	}
	return tmp, nil
}

func (m mockFS) OpenFile(path string, _ int, _ os.FileMode) (*os.File, error) {
	if _, ok := m.files[path]; !ok {
		return nil, os.ErrNotExist
	}
	return nil, nil
}

func (m mockFS) WriteFile(path string, content []byte, _ os.FileMode) error {
	m.files[path] = string(content)
	return nil
}

func (m mockFS) Remove(path string) error {
	if _, ok := m.files[path]; !ok {
		return os.ErrNotExist
	}
	delete(m.files, path)
	return nil
}

func (s *ConfigTestSuite) SetupTest() {
	s.fs = mockFS{
		files: make(map[string]string),
	}
	s.provider = config.NewWithPath(s.fs, "test/config.yaml")
}

func (s *ConfigTestSuite) TestLoadDefaultWhenNoFile() {
	// When loading configuration with no file present
	cfg, err := s.provider.Load()

	// Then default configuration should be returned
	s.Require().NoError(err)
	s.Equal(config.DefaultSocketPath, cfg.Socket.Path)
	s.Equal(config.DefaultRefreshInterval, cfg.Rules.RefreshInterval)
	s.Equal(config.DefaultDNSTimeout, cfg.Rules.DNSTimeout)
}

func (s *ConfigTestSuite) TestLoadValidConfig() {
	// Given a valid config file
	s.fs.files["test/config.yaml"] = `
socket:
  path: /custom/socket
rules:
  dns_refresh_interval: 2h
  dns_timeout: 10s
`
	// When loading configuration
	cfg, err := s.provider.Load()

	// Then custom values should be loaded
	s.Require().NoError(err)
	s.Equal("/custom/socket", cfg.Socket.Path)
	s.Equal(2*time.Hour, cfg.Rules.RefreshInterval)
	s.Equal(10*time.Second, cfg.Rules.DNSTimeout)
}

func (s *ConfigTestSuite) TestValidation() {
	testCases := []struct {
		name        string
		config      config.Config
		expectedErr string
	}{
		// Socket Path Validation
		{
			name: "empty socket path",
			config: config.Config{
				Socket: config.SocketConfig{Path: ""},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "socket path cannot be empty",
		},
		{
			name: "socket path only whitespace",
			config: config.Config{
				Socket: config.SocketConfig{Path: "   \t\n"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "socket path cannot be empty",
		},

		// RefreshInterval Validation
		{
			name: "refresh interval zero",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: 0,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "refresh interval must be at least 1 minute",
		},
		{
			name: "refresh interval negative",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: -time.Hour,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "refresh interval must be at least 1 minute",
		},
		{
			name: "refresh interval too short",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Second * 30,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "refresh interval must be at least 1 minute",
		},
		{
			name: "refresh interval exactly 1 minute",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Minute,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "",
		},

		// DNS Timeout Validation
		{
			name: "DNS timeout zero",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      0,
				},
			},
			expectedErr: "DNS timeout must be at least 1 second",
		},
		{
			name: "DNS timeout negative",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      -time.Second,
				},
			},
			expectedErr: "DNS timeout must be at least 1 second",
		},
		{
			name: "DNS timeout too short",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      time.Millisecond * 500,
				},
			},
			expectedErr: "DNS timeout must be at least 1 second",
		},
		{
			name: "DNS timeout exactly 1 second",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      time.Second,
				},
			},
			expectedErr: "",
		},

		// Combined Validation
		{
			name: "multiple validation errors",
			config: config.Config{
				Socket: config.SocketConfig{Path: ""},
				Rules: config.RulesConfig{
					RefreshInterval: time.Second * 30,
					DNSTimeout:      time.Millisecond * 500,
				},
			},
			expectedErr: "socket path cannot be empty", // First error encountered
		},
		{
			name: "all fields valid minimum values",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Minute,
					DNSTimeout:      time.Second,
				},
			},
			expectedErr: "",
		},
		{
			name: "all fields valid typical values",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour,
					DNSTimeout:      time.Second * 5,
				},
			},
			expectedErr: "",
		},
		{
			name: "all fields valid maximum reasonable values",
			config: config.Config{
				Socket: config.SocketConfig{Path: "/tmp/socket"},
				Rules: config.RulesConfig{
					RefreshInterval: time.Hour * 24,
					DNSTimeout:      time.Second * 30,
				},
			},
			expectedErr: "",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := tc.config.Validate()
			if tc.expectedErr == "" {
				s.NoError(err)
			} else {
				s.Error(err)
				s.Contains(err.Error(), tc.expectedErr)
			}
		})
	}
}

func (s *ConfigTestSuite) TestLoadInvalidYAML() {
	// Given an invalid YAML file
	s.fs.files["test/config.yaml"] = `
socket:
  path: [invalid: yaml]
`
	// When loading configuration
	_, err := s.provider.Load()

	// Then an error should be returned
	s.Error(err)
	s.Contains(err.Error(), "decoding config file")
}

func TestConfigSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
