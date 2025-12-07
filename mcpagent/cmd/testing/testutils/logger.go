package testutils

import (
	loggerv2 "mcpagent/logger/v2"

	"github.com/spf13/viper"
)

// TestLoggerConfig holds configuration for test logger initialization
type TestLoggerConfig struct {
	LogFile  string // Optional log file path
	LogLevel string // Log level (debug, info, warn, error)
	Format   string // Log format (text, json) - defaults to "text"
	Output   string // Output destination (stdout, file, both) - defaults to "stdout"
}

// NewTestLogger creates a new test logger with the specified configuration.
// If config is nil or empty, it uses viper to get configuration from flags.
// Falls back to sensible defaults if configuration is missing.
func NewTestLogger(cfg *TestLoggerConfig) loggerv2.Logger {
	// Use viper if config is not provided
	if cfg == nil {
		cfg = &TestLoggerConfig{}
	}

	// Get values from viper if not set in config
	if cfg.LogFile == "" {
		cfg.LogFile = viper.GetString("log-file")
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = viper.GetString("log-level")
		if cfg.LogLevel == "" {
			cfg.LogLevel = "info"
		}
	}
	if cfg.Format == "" {
		cfg.Format = "text"
	}

	// Determine output destination
	// If log file is specified, write only to file (not stdout)
	// Otherwise, write to stdout
	var output string
	var enableFile bool
	var filePath string

	if cfg.LogFile != "" {
		// Log file specified: write only to file
		output = cfg.LogFile
		enableFile = false
		filePath = ""
	} else {
		// No log file: write to stdout
		if cfg.Output == "" {
			output = "stdout"
		} else {
			output = cfg.Output
		}
		enableFile = false
		filePath = ""
	}

	// Create logger config
	loggerCfg := loggerv2.Config{
		Level:      cfg.LogLevel,
		Format:     cfg.Format,
		Output:     output,
		EnableFile: enableFile,
		FilePath:   filePath,
	}

	// Create logger
	l, err := loggerv2.New(loggerCfg)
	if err != nil {
		// Fallback to noop logger if creation fails
		return loggerv2.NewNoop()
	}

	return l
}

// NewTestLoggerFromViper creates a test logger using viper configuration.
// This is a convenience function that calls NewTestLogger(nil).
func NewTestLoggerFromViper() loggerv2.Logger {
	return NewTestLogger(nil)
}
