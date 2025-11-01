package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Logger implements utils.ExtendedLogger interface
// This is a clean implementation without global state
type Logger struct {
	logger *logrus.Logger
	file   *os.File
}

// CreateLogger creates a new logger instance with specified configuration
// This replaces the deprecated utils.InitLogger() function
func CreateLogger(logFile string, level string, format string, enableStdout bool) (Logger, error) {
	// Create new logrus logger
	logrusLogger := logrus.New()

	// Set log level
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		return Logger{}, fmt.Errorf("invalid log level: %w", err)
	}
	logrusLogger.SetLevel(logLevel)

	// Set formatter
	switch strings.ToLower(format) {
	case "json":
		logrusLogger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				filename := filepath.Base(f.File)
				return "", fmt.Sprintf("%s:%d", filename, f.Line)
			},
		})
	case "text":
		logrusLogger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				filename := filepath.Base(f.File)
				return "", fmt.Sprintf("%s:%d", filename, f.Line)
			},
		})
	default:
		return Logger{}, fmt.Errorf("unsupported log format: %s", format)
	}

	// Enable caller information
	logrusLogger.SetReportCaller(true)

	// Set up file logging if specified
	var file *os.File
	if logFile != "" {
		// Create log directory if it doesn't exist
		logDir := filepath.Dir(logFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return Logger{}, fmt.Errorf("failed to create log directory: %w", err)
		}

		// Open log file
		//nolint:gosec // G304: logFile comes from configuration/environment, not user input
		file, err = os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return Logger{}, fmt.Errorf("failed to open log file: %w", err)
		}

		// Set output to file when log file is specified
		logrusLogger.SetOutput(file)
	} else {
		// Default to file logging when no log file is specified
		// Create a default log file in logs/ directory
		defaultLogFile := fmt.Sprintf("logs/mcp-agent-%s.log", time.Now().Format("2006-01-02"))

		// Create logs directory if it doesn't exist
		if err := os.MkdirAll("logs", 0755); err != nil {
			return Logger{}, fmt.Errorf("failed to create default logs directory: %w", err)
		}

		// Open default log file
		//nolint:gosec // G304: defaultLogFile is generated internally with controlled format
		file, err = os.OpenFile(defaultLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return Logger{}, fmt.Errorf("failed to open default log file: %w", err)
		}

		// Set output to default log file
		logrusLogger.SetOutput(file)
	}

	// If stdout is explicitly enabled, add it as an additional output
	if enableStdout {
		// Use multi-writer to write to both file and stdout
		multiWriter := io.MultiWriter(file, os.Stdout)
		logrusLogger.SetOutput(multiWriter)
	}

	// Create and return logger instance
	return Logger{
		logger: logrusLogger,
		file:   file,
	}, nil
}

// CreateTestLogger creates a simplified test logger
func CreateTestLogger(logFile string, level string) Logger {
	logger, err := CreateLogger(logFile, level, "text", false)
	if err != nil {
		// Fallback to default logger if there's an error
		logger, _ = CreateLogger("logs/test-fallback.log", "info", "text", false)
	}
	return logger
}

// CreateDefaultLogger creates logger with sensible defaults
func CreateDefaultLogger() Logger {
	return CreateTestLogger("logs/default.log", "info")
}

// CreateDebugLogger creates logger with debug level and console output
func CreateDebugLogger(logFile string) Logger {
	logger, err := CreateLogger(logFile, "debug", "text", true)
	if err != nil {
		// Fallback to default logger if there's an error
		logger, _ = CreateLogger("logs/debug-fallback.log", "debug", "text", true)
	}
	return logger
}

// Implement utils.ExtendedLogger interface methods

func (l Logger) Infof(format string, v ...any) {
	l.logger.Infof(format, v...)
}

func (l Logger) Errorf(format string, v ...any) {
	l.logger.Errorf(format, v...)
}

func (l Logger) Info(args ...interface{}) {
	l.logger.Info(args...)
}

func (l Logger) Error(args ...interface{}) {
	l.logger.Error(args...)
}

func (l Logger) Debug(args ...interface{}) {
	l.logger.Debug(args...)
}

func (l Logger) Debugf(format string, args ...interface{}) {
	l.logger.Debugf(format, args...)
}

func (l Logger) Warn(args ...interface{}) {
	l.logger.Warn(args...)
}

func (l Logger) Warnf(format string, args ...interface{}) {
	l.logger.Warnf(format, args...)
}

func (l Logger) Fatal(args ...interface{}) {
	l.logger.Fatal(args...)
}

func (l Logger) Fatalf(format string, args ...interface{}) {
	l.logger.Fatalf(format, args...)
}

func (l Logger) WithField(key string, value interface{}) *logrus.Entry {
	return l.logger.WithField(key, value)
}

func (l Logger) WithFields(fields logrus.Fields) *logrus.Entry {
	return l.logger.WithFields(fields)
}

func (l Logger) WithError(err error) *logrus.Entry {
	return l.logger.WithError(err)
}

// Close closes the logger and any open files
func (l Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// IsInitialized returns true if the logger has been properly initialized
func (l Logger) IsInitialized() bool {
	return l.logger != nil
}
