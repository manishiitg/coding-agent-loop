package server

import (
	"mcp-agent-builder-go/agent_go/internal/utils"
	loggerv2 "mcpagent/logger/v2"

	"github.com/sirupsen/logrus"
)

// adaptExtendedLoggerToV2 converts utils.ExtendedLogger to loggerv2.Logger
func adaptExtendedLoggerToV2(extLogger utils.ExtendedLogger) loggerv2.Logger {
	return &extendedLoggerToV2Adapter{
		logger: extLogger,
	}
}

// extendedLoggerToV2Adapter adapts utils.ExtendedLogger to loggerv2.Logger
type extendedLoggerToV2Adapter struct {
	logger utils.ExtendedLogger
}

func (a *extendedLoggerToV2Adapter) Debug(msg string, fields ...loggerv2.Field) {
	if len(fields) > 0 {
		logrusFields := make(logrus.Fields, len(fields))
		for _, field := range fields {
			logrusFields[field.Key] = field.Value
		}
		entry := a.logger.WithFields(logrusFields)
		entry.Debug(msg)
	} else {
		a.logger.Debug(msg)
	}
}

func (a *extendedLoggerToV2Adapter) Info(msg string, fields ...loggerv2.Field) {
	if len(fields) > 0 {
		logrusFields := make(logrus.Fields, len(fields))
		for _, field := range fields {
			logrusFields[field.Key] = field.Value
		}
		entry := a.logger.WithFields(logrusFields)
		entry.Info(msg)
	} else {
		a.logger.Info(msg)
	}
}

func (a *extendedLoggerToV2Adapter) Warn(msg string, fields ...loggerv2.Field) {
	if len(fields) > 0 {
		logrusFields := make(logrus.Fields, len(fields))
		for _, field := range fields {
			logrusFields[field.Key] = field.Value
		}
		entry := a.logger.WithFields(logrusFields)
		entry.Warn(msg)
	} else {
		a.logger.Warn(msg)
	}
}

func (a *extendedLoggerToV2Adapter) Error(msg string, err error, fields ...loggerv2.Field) {
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

func (a *extendedLoggerToV2Adapter) Fatal(msg string, err error, fields ...loggerv2.Field) {
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

func (a *extendedLoggerToV2Adapter) With(fields ...loggerv2.Field) loggerv2.Logger {
	return &extendedLoggerToV2AdapterWithFields{
		baseAdapter: a,
		fields:      fields,
	}
}

func (a *extendedLoggerToV2Adapter) Close() error {
	return a.logger.Close()
}

// extendedLoggerToV2AdapterWithFields is a child logger with preset fields
type extendedLoggerToV2AdapterWithFields struct {
	baseAdapter *extendedLoggerToV2Adapter
	fields      []loggerv2.Field
}

func (a *extendedLoggerToV2AdapterWithFields) Debug(msg string, fields ...loggerv2.Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Debug(msg, allFields...)
}

func (a *extendedLoggerToV2AdapterWithFields) Info(msg string, fields ...loggerv2.Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Info(msg, allFields...)
}

func (a *extendedLoggerToV2AdapterWithFields) Warn(msg string, fields ...loggerv2.Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Warn(msg, allFields...)
}

func (a *extendedLoggerToV2AdapterWithFields) Error(msg string, err error, fields ...loggerv2.Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Error(msg, err, allFields...)
}

func (a *extendedLoggerToV2AdapterWithFields) Fatal(msg string, err error, fields ...loggerv2.Field) {
	allFields := append(a.fields, fields...)
	a.baseAdapter.Fatal(msg, err, allFields...)
}

func (a *extendedLoggerToV2AdapterWithFields) With(fields ...loggerv2.Field) loggerv2.Logger {
	return &extendedLoggerToV2AdapterWithFields{
		baseAdapter: a.baseAdapter,
		fields:      append(a.fields, fields...),
	}
}

func (a *extendedLoggerToV2AdapterWithFields) Close() error {
	return a.baseAdapter.Close()
}
