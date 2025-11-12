package testing

import (
	"fmt"
	"io"
	"llm-providers/interfaces"
	"os"
)

// SimpleLogger is a basic logger implementation for testing
type SimpleLogger struct {
	output io.Writer
	level  string
}

var testLogger interfaces.Logger

// InitTestLogger initializes a simple test logger
func InitTestLogger(logFile string, level string) {
	var output io.Writer = os.Stdout
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			output = file
		}
	}
	testLogger = &SimpleLogger{
		output: output,
		level:  level,
	}
}

// GetTestLogger returns the shared test logger instance
func GetTestLogger() interfaces.Logger {
	if testLogger == nil {
		testLogger = &SimpleLogger{
			output: os.Stdout,
			level:  "info",
		}
	}
	return testLogger
}

// SetTestLogger allows tests to override the shared logger
func SetTestLogger(logger interfaces.Logger) {
	testLogger = logger
}

// SimpleLogger implementation
func (l *SimpleLogger) Infof(format string, v ...any) {
	fmt.Fprintf(l.output, "[INFO] "+format+"\n", v...)
}

func (l *SimpleLogger) Errorf(format string, v ...any) {
	fmt.Fprintf(l.output, "[ERROR] "+format+"\n", v...)
}

func (l *SimpleLogger) Info(args ...interface{}) {
	fmt.Fprint(l.output, "[INFO] ")
	fmt.Fprintln(l.output, args...)
}

func (l *SimpleLogger) Error(args ...interface{}) {
	fmt.Fprint(l.output, "[ERROR] ")
	fmt.Fprintln(l.output, args...)
}

func (l *SimpleLogger) Debug(args ...interface{}) {
	if l.level == "debug" {
		fmt.Fprint(l.output, "[DEBUG] ")
		fmt.Fprintln(l.output, args...)
	}
}

func (l *SimpleLogger) Debugf(format string, args ...interface{}) {
	if l.level == "debug" {
		fmt.Fprintf(l.output, "[DEBUG] "+format+"\n", args...)
	}
}

func (l *SimpleLogger) Warn(args ...interface{}) {
	fmt.Fprint(l.output, "[WARN] ")
	fmt.Fprintln(l.output, args...)
}

func (l *SimpleLogger) Warnf(format string, args ...interface{}) {
	fmt.Fprintf(l.output, "[WARN] "+format+"\n", args...)
}

func (l *SimpleLogger) Fatal(args ...interface{}) {
	fmt.Fprint(l.output, "[FATAL] ")
	fmt.Fprintln(l.output, args...)
	os.Exit(1)
}

func (l *SimpleLogger) Fatalf(format string, args ...interface{}) {
	fmt.Fprintf(l.output, "[FATAL] "+format+"\n", args...)
	os.Exit(1)
}

func (l *SimpleLogger) WithField(key string, value interface{}) interface{} {
	return l
}

func (l *SimpleLogger) WithFields(fields map[string]interface{}) interface{} {
	return l
}

func (l *SimpleLogger) WithError(err error) interface{} {
	return l
}

func (l *SimpleLogger) Close() error {
	if closer, ok := l.output.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
