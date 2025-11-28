package mcpclient

import (
	"mcpagent/logger"
)

// FileLoggerAdapter adapts our logger.ExtendedLogger to the mcp-go util.Logger interface
type FileLoggerAdapter struct {
	logger logger.ExtendedLogger
}

func (l *FileLoggerAdapter) Infof(format string, args ...interface{}) {
	l.logger.Infof(format, args...)
}

func (l *FileLoggerAdapter) Errorf(format string, args ...interface{}) {
	l.logger.Errorf(format, args...)
}
