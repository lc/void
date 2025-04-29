// Package config provides configuration management for the void domain blocking system.
//
// The package uses a Provider interface to abstract configuration loading, with the
// primary implementation being filesystem-based configuration via YAML files.
//
// # Configuration Structure
//
// Configuration is structured as follows:
//
//	socket:
//	  path: /var/run/voidd.socket     # Unix domain socket path
//	rules:
//	  dns_refresh_interval: 1h            # How often to refresh DNS records
//	  dns_timeout: 5s                 # Timeout for DNS queries
//
// # Basic Usage
//
// Load configuration using the default path (~/.void/config.yaml):
//
//	provider := config.New("")
//	cfg, err := provider.Load()
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Load configuration from a specific path:
//
//	provider := config.New("/etc/void/config.yaml")
//	cfg, err := provider.Load()
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Configuration Validation
//
// The package performs validation of loaded configuration:
//   - Socket path must not be empty
//   - Refresh interval must be at least 1 minute
//   - DNS timeout must be at least 1 second
//
// # Default Configuration
//
// If no configuration file exists, the following defaults are used:
//   - Socket Path: /var/run/voidd.socket
//   - Refresh Interval: 1 hour
//   - DNS Timeout: 5 seconds
//
// # Thread Safety
//
// Configuration loading is thread-safe. However, once loaded, the Config
// struct should be treated as immutable. If configuration changes are needed,
// a new Config should be loaded.
//
// # Error Handling
//
// The package defines several error types:
//   - ErrInvalidConfig: Configuration validation failed
//   - ErrNoConfig: Configuration file not found (returns defaults)
//
// Example configuration file:
//
//	# ~/.void/config.yaml
//	socket:
//	  path: /var/run/voidd.socket
//	rules:
//	  dns_refresh_interval: 1h
//	  dns_timeout: 5s
//
// The package is designed to be extensible, allowing for additional
// configuration providers to be implemented (e.g., environment variables,
// remote configuration services) by implementing the Provider interface.
package config
