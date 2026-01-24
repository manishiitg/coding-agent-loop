package logger

import (
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// CreateLogger creates a new logger instance with specified configuration
// Returns loggerv2.Logger for consistency with mcpagent library
func CreateLogger(logFile string, level string, format string, enableStdout bool) (loggerv2.Logger, error) {
	cfg := loggerv2.Config{
		Level:      level,
		Format:     format,
		EnableFile: logFile != "",
		FilePath:   logFile,
	}

	// Determine output destination
	if enableStdout {
		cfg.Output = "stdout"
	} else if logFile != "" {
		// Do not set Output to logFile to avoid duplication as EnableFile handles file output
	} else {
		// Default to stdout when no log file is specified and stdout is disabled
		cfg.Output = "stdout"
	}

	return loggerv2.New(cfg)
}

// CreateTestLogger creates a simplified test logger
func CreateTestLogger(logFile string, level string) loggerv2.Logger {
	logger, err := CreateLogger(logFile, level, "text", false)
	if err != nil {
		// Fallback to default logger if there's an error
		logger, _ = CreateLogger("logs/test-fallback.log", "info", "text", false)
	}
	return logger
}

// CreateDefaultLogger creates logger with sensible defaults
func CreateDefaultLogger() loggerv2.Logger {
	return CreateTestLogger("logs/default.log", "info")
}

// CreateDebugLogger creates logger with debug level and console output
func CreateDebugLogger(logFile string) loggerv2.Logger {
	logger, err := CreateLogger(logFile, "debug", "text", true)
	if err != nil {
		// Fallback to default logger if there's an error
		logger, _ = CreateLogger("logs/debug-fallback.log", "debug", "text", true)
	}
	return logger
}
