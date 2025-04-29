// Package log provides a simplified logging interface for the Void application.
// It wraps go.uber.org/zap to provide a consistent logging experience with
// sensible defaults and convenient helper functions for different log levels.
package log

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the global logger instance.
// It's configured for development-friendly output by default.
var Logger = newLogger()

func newLogger() *zap.SugaredLogger {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	LOG_LEVEL := os.Getenv("LOG_LEVEL")
	if LOG_LEVEL != "" {
		switch LOG_LEVEL {
		case "debug":
			cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		default:
		}
	}
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.DisableCaller = true

	l, err := cfg.Build()
	if err != nil {
		// If we can't build the logger, fall back to a no-op logger
		// This should never happen with the default config, but we handle it anyway
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return zap.NewNop().Sugar()
	}
	return l.Sugar()
}

// Info logs a message at info level with optional key-value pairs.
func Info(msg string, kv ...any) { Logger.Infow(msg, kv...) }

// Infof logs a formatted message at info level.
func Infof(format string, a ...any) { Logger.Infof(format, a...) }

// Warn logs a message at warn level with optional key-value pairs.
func Warn(msg string, kv ...any) { Logger.Warnw(msg, kv...) }

// Warnf logs a formatted message at warn level.
func Warnf(format string, a ...any) { Logger.Warnf(format, a...) }

// Error logs a message at error level with optional key-value pairs.
func Error(msg string, kv ...any) { Logger.Errorw(msg, kv...) }

// Errorf logs a formatted message at error level.
func Errorf(format string, a ...any) { Logger.Errorf(format, a...) }

// Debug logs a message at debug level with optional key-value pairs.
func Debug(msg string, kv ...any) { Logger.Debugw(msg, kv...) }

// Debugf logs a formatted message at debug level.
func Debugf(format string, a ...any) { Logger.Debugf(format, a...) }

// Fatal logs a message at fatal level with optional key-value pairs,
// then calls os.Exit(1).
func Fatal(msg string, kv ...any) { Logger.Fatalw(msg, kv...) }

// Fatalf logs a formatted message at fatal level, then calls os.Exit(1).
func Fatalf(format string, a ...any) { Logger.Fatalf(format, a...) }
