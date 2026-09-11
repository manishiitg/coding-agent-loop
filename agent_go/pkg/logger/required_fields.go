package logger

import loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

// requiredFieldsLogger adds inherited fields without losing ownership of the
// underlying logger's file handle (loggerv2 child loggers intentionally do not
// own/close that handle).
type requiredFieldsLogger struct {
	base   loggerv2.Logger
	fields []loggerv2.Field
	owns   bool
}

func withRequiredFields(base loggerv2.Logger, owns bool, fields ...loggerv2.Field) loggerv2.Logger {
	return &requiredFieldsLogger{base: base, fields: append([]loggerv2.Field(nil), fields...), owns: owns}
}

func (l *requiredFieldsLogger) combined(fields []loggerv2.Field) []loggerv2.Field {
	combined := make([]loggerv2.Field, 0, len(l.fields)+len(fields))
	combined = append(combined, l.fields...)
	combined = append(combined, fields...)
	return combined
}

func (l *requiredFieldsLogger) Debug(msg string, fields ...loggerv2.Field) {
	l.base.Debug(msg, l.combined(fields)...)
}

func (l *requiredFieldsLogger) Info(msg string, fields ...loggerv2.Field) {
	l.base.Info(msg, l.combined(fields)...)
}

func (l *requiredFieldsLogger) Warn(msg string, fields ...loggerv2.Field) {
	l.base.Warn(msg, l.combined(fields)...)
}

func (l *requiredFieldsLogger) Error(msg string, err error, fields ...loggerv2.Field) {
	l.base.Error(msg, err, l.combined(fields)...)
}

func (l *requiredFieldsLogger) Fatal(msg string, err error, fields ...loggerv2.Field) {
	l.base.Fatal(msg, err, l.combined(fields)...)
}

func (l *requiredFieldsLogger) With(fields ...loggerv2.Field) loggerv2.Logger {
	return withRequiredFields(l.base, false, l.combined(fields)...)
}

func (l *requiredFieldsLogger) Close() error {
	if !l.owns {
		return nil
	}
	return l.base.Close()
}
