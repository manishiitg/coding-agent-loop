package mcpclient

import (
	"mcp-agent/agent_go/internal/utils"
)

// FileLoggerAdapter adapts our utils.ExtendedLogger to the mcp-go util.Logger interface
type FileLoggerAdapter struct {
	logger utils.ExtendedLogger
}

func (l *FileLoggerAdapter) Infof(format string, args ...interface{}) {
	l.logger.Infof(format, args...)
}

func (l *FileLoggerAdapter) Errorf(format string, args ...interface{}) {
	l.logger.Errorf(format, args...)
}
