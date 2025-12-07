package logger

import (
	"github.com/mark3labs/mcp-go/util"
	"github.com/sirupsen/logrus"
)

// ExtendedLogger is our own interface that includes all the methods we need
// This interface is implemented by logger.Logger from pkg/logger package
//
// Deprecated: ExtendedLogger is deprecated and will be removed in a future version.
// Use logger/v2.Logger instead for new code. This interface is kept only for
// backward compatibility with existing code and MCP-Go library compatibility.
type ExtendedLogger interface {
	// Core MCP-Go compatibility methods
	Infof(format string, v ...any)
	Errorf(format string, v ...any)

	// Additional methods we need
	Info(args ...interface{})
	Error(args ...interface{})
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})

	// Structured logging methods
	WithField(key string, value interface{}) *logrus.Entry
	WithFields(fields logrus.Fields) *logrus.Entry
	WithError(err error) *logrus.Entry

	// File management
	Close() error
}

// AdaptLogger adapts a util.Logger (MCP-Go interface) to our ExtendedLogger interface
// This provides backward compatibility for external packages that only have util.Logger
//
// Deprecated: Use logger/v2.FromUtilLogger() or logger/v2.ToUtilLogger() instead.
// This function is kept only for backward compatibility.
func AdaptLogger(logger util.Logger) ExtendedLogger {
	return &LoggerAdapter{
		logger: logger,
	}
}

// AdaptToUtilLogger adapts our ExtendedLogger to util.Logger (MCP-Go interface)
// This provides compatibility for packages that expect util.Logger
//
// Deprecated: Use logger/v2.ToUtilLogger() instead. This function is kept only
// for backward compatibility.
func AdaptToUtilLogger(logger ExtendedLogger) util.Logger {
	return &ReverseLoggerAdapter{
		logger: logger,
	}
}

// LoggerAdapter adapts util.Logger to ExtendedLogger
type LoggerAdapter struct {
	logger util.Logger
}

// ReverseLoggerAdapter adapts our ExtendedLogger to util.Logger
type ReverseLoggerAdapter struct {
	logger ExtendedLogger
}

// Implement ExtendedLogger interface methods
func (a *LoggerAdapter) Infof(format string, v ...any) {
	a.logger.Infof(format, v...)
}

func (a *LoggerAdapter) Errorf(format string, v ...any) {
	a.logger.Errorf(format, v...)
}

func (a *LoggerAdapter) Info(args ...interface{}) {
	// Convert to Infof call
	a.logger.Infof("%v", args...)
}

func (a *LoggerAdapter) Error(args ...interface{}) {
	// Convert to Errorf call
	a.logger.Errorf("%v", args...)
}

func (a *LoggerAdapter) Debug(args ...interface{}) {
	// Convert to Infof call (util.Logger doesn't have Debug)
	a.logger.Infof("[DEBUG] %v", args...)
}

func (a *LoggerAdapter) Debugf(format string, args ...interface{}) {
	// Convert to Infof call (util.Logger doesn't have Debugf)
	a.logger.Infof("[DEBUG] "+format, args...)
}

func (a *LoggerAdapter) Warn(args ...interface{}) {
	// Convert to Infof call (util.Logger doesn't have Warn)
	a.logger.Infof("[WARN] %v", args...)
}

func (a *LoggerAdapter) Warnf(format string, args ...interface{}) {
	// Convert to Infof call (util.Logger doesn't have Warnf)
	a.logger.Infof("[WARN] "+format, args...)
}

func (a *LoggerAdapter) Fatal(args ...interface{}) {
	// Convert to Errorf call (util.Logger doesn't have Fatal)
	a.logger.Errorf("[FATAL] %v", args...)
}

func (a *LoggerAdapter) Fatalf(format string, args ...interface{}) {
	// Convert to Errorf call (util.Logger doesn't have Fatalf)
	a.logger.Errorf("[FATAL] "+format, args...)
}

func (a *LoggerAdapter) WithField(key string, value interface{}) *logrus.Entry {
	// util.Logger doesn't support structured logging, so we create a simple entry
	entry := logrus.NewEntry(logrus.StandardLogger())
	return entry.WithField(key, value)
}

func (a *LoggerAdapter) WithFields(fields logrus.Fields) *logrus.Entry {
	// util.Logger doesn't support structured logging, so we create a simple entry
	entry := logrus.NewEntry(logrus.StandardLogger())
	return entry.WithFields(fields)
}

func (a *LoggerAdapter) WithError(err error) *logrus.Entry {
	// util.Logger doesn't support structured logging, so we create a simple entry
	entry := logrus.NewEntry(logrus.StandardLogger())
	return entry.WithError(err)
}

func (a *LoggerAdapter) Close() error {
	// util.Logger doesn't support file management, so we do nothing
	return nil
}

// Implement util.Logger interface methods for ReverseLoggerAdapter
func (a *ReverseLoggerAdapter) Infof(format string, v ...any) {
	a.logger.Infof(format, v...)
}

func (a *ReverseLoggerAdapter) Errorf(format string, v ...any) {
	a.logger.Errorf(format, v...)
}
