// Package config provides configuration loading and validation for the Void application.
// It handles reading configuration from files, providing defaults, and ensuring
// all required settings are properly set.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lc/void/internal/filesys"
)

var (
	// ErrInvalidConfig is returned when the configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")
	// ErrNoConfig is returned when the configuration file is not found.
	ErrNoConfig = errors.New("configuration file not found")
)

const (
	// DefaultSocketPath is the default path for the Unix socket.
	DefaultSocketPath = "/var/run/voidd.socket"
	// DefaultConfigPath is the default path for the configuration file.
	DefaultConfigPath = ".void/config.yaml"
	// DefaultRefreshInterval is the default interval for refreshing DNS records.
	DefaultRefreshInterval = 5 * time.Minute
	// DefaultDNSTimeout is the default timeout for DNS resolution.
	DefaultDNSTimeout = 5 * time.Second
)

// Config holds the application configuration.
type Config struct {
	Socket SocketConfig `yaml:"socket"`
	Rules  RulesConfig  `yaml:"rules"`
}

// SocketConfig holds socket-related configuration.
type SocketConfig struct {
	Path string `yaml:"path"`
}

// RulesConfig holds rule-related configuration.
type RulesConfig struct {
	RefreshInterval time.Duration `yaml:"dns_refresh_interval"`
	DNSTimeout      time.Duration `yaml:"dns_timeout"`
}

// Provider defines the interface for loading configuration.
type Provider interface {
	Load() (*Config, error)
}

// FSProvider implements Provider using the local filesystem.
type FSProvider struct {
	fs   filesys.ReadWriteFS
	path string
}

// Verify FSProvider implements Provider interface.
var _ Provider = (*FSProvider)(nil)

// New creates a new configuration provider using the default configuration path.
// It uses the OS filesystem and the user's home directory to locate the configuration file.
// If the home directory cannot be determined, it falls back to the current directory.
func New() Provider {
	home, err := os.UserHomeDir()
	if err != nil {
		// Log the error but continue with empty path, which will resolve to current directory
		fmt.Fprintf(os.Stderr, "Warning: could not determine home directory: %v\n", err)
		home = ""
	}
	return NewWithPath(filesys.OS(), filepath.Join(home, DefaultConfigPath))
}

// NewWithPath creates a new provider with a specific config path.
// It allows specifying both the filesystem implementation and the path to use.
func NewWithPath(fs filesys.ReadWriteFS, path string) Provider {
	return &FSProvider{
		fs:   fs,
		path: path,
	}
}

// Default returns a default configuration with preset values.
// This is used when no configuration file exists.
func Default() *Config {
	return &Config{
		Socket: SocketConfig{
			Path: DefaultSocketPath,
		},
		Rules: RulesConfig{
			RefreshInterval: DefaultRefreshInterval,
			DNSTimeout:      DefaultDNSTimeout,
		},
	}
}

// Load loads the configuration from the specified path.
func (p *FSProvider) Load() (*Config, error) {
	_ = p.ensureConfigDir()

	cfg, err := p.loadAndParse()
	if err != nil {
		if errors.Is(err, ErrNoConfig) {
			return Default(), nil
		}
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	return cfg, nil
}

// Validate checks the configuration to ensure all required fields are set.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Socket.Path) == "" {
		return errors.New("socket path cannot be empty")
	}
	if c.Rules.RefreshInterval < time.Minute {
		return errors.New("refresh interval must be at least 1 minute")
	}
	if c.Rules.DNSTimeout < time.Second {
		return errors.New("DNS timeout must be at least 1 second")
	}
	return nil
}

func (p *FSProvider) ensureConfigDir() error {
	dir := filepath.Dir(p.path)
	if _, err := p.fs.Stat(dir); os.IsNotExist(err) {
		if err := p.fs.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
	}
	return nil
}

func (p *FSProvider) loadAndParse() (*Config, error) {
	f, err := p.fs.Open(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoConfig
		}
		return nil, fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding config file: %w", err)
	}

	return &cfg, nil
}
