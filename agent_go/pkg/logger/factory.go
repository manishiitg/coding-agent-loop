package logger

import (
	"context"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

const missingLogContextValue = "-"

// CreateLogger creates a new logger instance with specified configuration
// Returns loggerv2.Logger for consistency with mcpagent library
func CreateLogger(logFile string, level string, format string, enableStdout bool) (loggerv2.Logger, error) {
	return createLoggerWithFields(logFile, level, format, enableStdout, diagnosticContextFields(nil))
}

// CreateLoggerWithContext creates an owning logger whose required identity
// fields are initialized from ctx. Prefer this over calling WithContext on a
// freshly-created file logger, because loggerv2 child loggers intentionally do
// not own the file handle.
func CreateLoggerWithContext(logFile string, level string, format string, enableStdout bool, ctx context.Context) (loggerv2.Logger, error) {
	return createLoggerWithFields(logFile, level, format, enableStdout, diagnosticContextFields(ctx))
}

func createLoggerWithFields(logFile string, level string, format string, enableStdout bool, fields []loggerv2.Field) (loggerv2.Logger, error) {
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

	created, err := loggerv2.New(cfg)
	if err != nil {
		return nil, err
	}
	// Stable fallback keys make every structured entry searchable. Request and
	// session child loggers overwrite these with real values when available.
	return withRequiredFields(created, true, fields...), nil
}

// WithContext copies diagnostic identity from an execution context onto a
// structured logger. These values are observational only; UserID remains the
// authorization identity.
func WithContext(base loggerv2.Logger, ctx context.Context) loggerv2.Logger {
	if base == nil {
		return nil
	}
	return base.With(diagnosticContextFields(ctx)...)
}

func diagnosticContextFields(ctx context.Context) []loggerv2.Field {
	username := missingLogContextValue
	workflow := missingLogContextValue
	if ctx != nil {
		if value, ok := ctx.Value(common.UsernameKey).(string); ok && value != "" {
			username = value
		}
		if value, ok := ctx.Value(common.WorkflowNameKey).(string); ok && value != "" {
			workflow = value
		}
	}
	return []loggerv2.Field{
		loggerv2.String("username", username),
		loggerv2.String("workflow", workflow),
	}
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
