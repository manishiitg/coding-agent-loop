package server

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const scheduleLogPath = "logs/schedule.log"

var (
	scheduleLoggerOnce sync.Once
	scheduleLogger     *log.Logger
)

func getScheduleLogger() *log.Logger {
	scheduleLoggerOnce.Do(func() {
		if err := os.MkdirAll(filepath.Dir(scheduleLogPath), 0755); err != nil {
			log.Printf("[SCHEDULER] Failed to create log directory for %s: %v", scheduleLogPath, err)
			scheduleLogger = log.Default()
			return
		}

		file, err := os.OpenFile(scheduleLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Printf("[SCHEDULER] Failed to open %s: %v", scheduleLogPath, err)
			scheduleLogger = log.Default()
			return
		}

		scheduleLogger = log.New(file, "", log.LstdFlags)
	})

	if scheduleLogger == nil {
		return log.Default()
	}
	return scheduleLogger
}

func scheduleLogf(format string, args ...interface{}) {
	getScheduleLogger().Printf(format, args...)
}

func isScheduledSession(sessionID string) bool {
	return strings.HasPrefix(sessionID, "sched_")
}
