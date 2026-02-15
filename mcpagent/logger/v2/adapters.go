package v2

import (
	"fmt"

	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/mark3labs/mcp-go/util"
	"github.com/sirupsen/logrus"

	"github.com/manishiitg/mcpagent/logger"
)

// ToExtendedLogger adapts the new v2.Logger to the legacy ExtendedLogger interface
// This allows gradual migration - new code can use v2.Logger, old code can still use ExtendedLogger
//
// Deprecated: ExtendedLogger is deprecated. Use v2.Logger directly in new code.
// This function is kept only for backward compatibility during migration.
func ToExtendedLogger(l Logger) logger.ExtendedLogger {
	return &extendedLoggerAdapter{
		logger: l,
	}
}

// extendedLoggerAdapter adapts v2.Logger to ExtendedLogger
type extendedLoggerAdapter struct {
	logger Logger
}

// Implement ExtendedLogger interface
func (a *extendedLoggerAdapter) Infof(format string, v ...any) {
	a.logger.Info(fmt.Sprintf(format, v...))
}

func (a *extendedLoggerAdapter) Errorf(format string, v ...any) {
	a.logger.Error(fmt.Sprintf(format, v...), nil)
}

func (a *extendedLoggerAdapter) Info(args ...interface{}) {
	a.logger.Info(fmt.Sprint(args...))
}

func (a *extendedLoggerAdapter) Error(args ...interface{}) {
	a.logger.Error(fmt.Sprint(args...), nil)
}

func (a *extendedLoggerAdapter) Debug(args ...interface{}) {
	a.logger.Debug(fmt.Sprint(args...))
}

func (a *extendedLoggerAdapter) Debugf(format string, args ...interface{}) {
	a.logger.Debug(fmt.Sprintf(format, args...))
}

func (a *extendedLoggerAdapter) Warn(args ...interface{}) {
	a.logger.Warn(fmt.Sprint(args...))
}

func (a *extendedLoggerAdapter) Warnf(format string, args ...interface{}) {
	a.logger.Warn(fmt.Sprintf(format, args...))
}

func (a *extendedLoggerAdapter) Fatal(args ...interface{}) {
	a.logger.Fatal(fmt.Sprint(args...), nil)
}

func (a *extendedLoggerAdapter) Fatalf(format string, args ...interface{}) {
	a.logger.Fatal(fmt.Sprintf(format, args...), nil)
}

// Note: WithField, WithFields, WithError return *logrus.Entry for compatibility
// This is a limitation of the ExtendedLogger interface that we're adapting to
// The new v2.Logger interface doesn't have this leakage
// We create a logrus entry for compatibility, but the actual logging should use the v2.Logger methods
func (a *extendedLoggerAdapter) WithField(key string, value interface{}) *logrus.Entry {
	// Store the field in the child logger (for future use if needed)
	_ = a.logger.With(Any(key, value))
	// Return logrus entry for compatibility
	entry := logrus.NewEntry(logrus.StandardLogger())
	return entry.WithField(key, value)
}

func (a *extendedLoggerAdapter) WithFields(fields logrus.Fields) *logrus.Entry {
	// Convert logrus.Fields to v2.Field slice and store in child logger
	v2Fields := make([]Field, 0, len(fields))
	for k, v := range fields {
		v2Fields = append(v2Fields, Any(k, v))
	}
	_ = a.logger.With(v2Fields...)
	// Return logrus entry for compatibility
	entry := logrus.NewEntry(logrus.StandardLogger())
	return entry.WithFields(fields)
}

func (a *extendedLoggerAdapter) WithError(err error) *logrus.Entry {
	// Store error in child logger
	_ = a.logger.With(Error(err))
	// Return logrus entry for compatibility
	entry := logrus.NewEntry(logrus.StandardLogger())
	return entry.WithError(err)
}

func (a *extendedLoggerAdapter) Close() error {
	return a.logger.Close()
}

// ToUtilLogger adapts v2.Logger to util.Logger (MCP-Go interface)
func ToUtilLogger(l Logger) util.Logger {
	return &utilLoggerAdapter{
		logger: l,
	}
}

// utilLoggerAdapter adapts v2.Logger to util.Logger
type utilLoggerAdapter struct {
	logger Logger
}

func (a *utilLoggerAdapter) Infof(format string, v ...any) {
	a.logger.Info(fmt.Sprintf(format, v...))
}

func (a *utilLoggerAdapter) Errorf(format string, v ...any) {
	a.logger.Error(fmt.Sprintf(format, v...), nil)
}

// ToInterfacesLogger adapts v2.Logger to interfaces.Logger (multi-llm-provider-go)
func ToInterfacesLogger(l Logger) interfaces.Logger {
	return &interfacesLoggerAdapter{
		logger: l,
	}
}

// interfacesLoggerAdapter adapts v2.Logger to interfaces.Logger
type interfacesLoggerAdapter struct {
	logger Logger
}

func (a *interfacesLoggerAdapter) Infof(format string, v ...any) {
	a.logger.Info(fmt.Sprintf(format, v...))
}

func (a *interfacesLoggerAdapter) Errorf(format string, v ...any) {
	a.logger.Error(fmt.Sprintf(format, v...), nil)
}

func (a *interfacesLoggerAdapter) Debugf(format string, args ...interface{}) {
	a.logger.Debug(fmt.Sprintf(format, args...))
}

// FromExtendedLogger adapts an ExtendedLogger to v2.Logger
// This allows existing code using ExtendedLogger to be migrated to v2.Logger
//
// Deprecated: ExtendedLogger is deprecated. Use v2.Logger directly in new code.
// This function is kept only for backward compatibility during migration.
func FromExtendedLogger(l logger.ExtendedLogger) Logger {
	return &extendedToV2Adapter{
		logger: l,
	}
}

// extendedToV2Adapter adapts ExtendedLogger to v2.Logger
type extendedToV2Adapter struct {
	logger logger.ExtendedLogger
}

func (a *extendedToV2Adapter) Debug(msg string, fields ...Field) {
	// Convert fields to logrus.Fields for ExtendedLogger
	logrusFields := make(logrus.Fields, len(fields))
	for _, field := range fields {
		logrusFields[field.Key] = field.Value
	}
	entry := a.logger.WithFields(logrusFields)
	entry.Debug(msg)
}

func (a *extendedToV2Adapter) Info(msg string, fields ...Field) {
	logrusFields := make(logrus.Fields, len(fields))
	for _, field := range fields {
		logrusFields[field.Key] = field.Value
	}
	entry := a.logger.WithFields(logrusFields)
	entry.Info(msg)
}

func (a *extendedToV2Adapter) Warn(msg string, fields ...Field) {
	logrusFields := make(logrus.Fields, len(fields))
	for _, field := range fields {
		logrusFields[field.Key] = field.Value
	}
	entry := a.logger.WithFields(logrusFields)
	entry.Warn(msg)
}

func (a *extendedToV2Adapter) Error(msg string, err error, fields ...Field) {
	logrusFields := make(logrus.Fields, len(fields))
	for _, field := range fields {
		logrusFields[field.Key] = field.Value
	}
	entry := a.logger.WithFields(logrusFields)
	if err != nil {
		entry = entry.WithError(err)
	}
	entry.Error(msg)
}

func (a *extendedToV2Adapter) Fatal(msg string, err error, fields ...Field) {
	logrusFields := make(logrus.Fields, len(fields))
	for _, field := range fields {
		logrusFields[field.Key] = field.Value
	}
	entry := a.logger.WithFields(logrusFields)
	if err != nil {
		entry = entry.WithError(err)
	}
	entry.Fatal(msg)
}

func (a *extendedToV2Adapter) With(fields ...Field) Logger {
	// Create a new adapter with the fields applied
	// We'll create a wrapper that applies these fields on each call
	return &extendedToV2AdapterWithFields{
		baseAdapter: a,
		fields:      fields,
	}
}

func (a *extendedToV2Adapter) Close() error {
	return a.logger.Close()
}

// extendedToV2AdapterWithFields is a child logger with preset fields
type extendedToV2AdapterWithFields struct {
	baseAdapter *extendedToV2Adapter
	fields      []Field
}

func (a *extendedToV2AdapterWithFields) Debug(msg string, fields ...Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Debug(msg, allFields...)
}

func (a *extendedToV2AdapterWithFields) Info(msg string, fields ...Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Info(msg, allFields...)
}

func (a *extendedToV2AdapterWithFields) Warn(msg string, fields ...Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Warn(msg, allFields...)
}

func (a *extendedToV2AdapterWithFields) Error(msg string, err error, fields ...Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Error(msg, err, allFields...)
}

func (a *extendedToV2AdapterWithFields) Fatal(msg string, err error, fields ...Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Fatal(msg, err, allFields...)
}

func (a *extendedToV2AdapterWithFields) With(fields ...Field) Logger {
	allFields := append(a.fields, fields...)
	return &extendedToV2AdapterWithFields{
		baseAdapter: a.baseAdapter,
		fields:      allFields,
	}
}

func (a *extendedToV2AdapterWithFields) Close() error {
	return a.baseAdapter.Close()
}
