package utils

import (
	"fmt"
	"sync"
	"time"
)

// FileLock represents an in-memory file lock
type FileLock struct {
	filepath string
	acquired time.Time
}

// LockManager manages in-memory file locks
type LockManager struct {
	locks map[string]*FileLock
	mutex sync.RWMutex
}

// NewLockManager creates a new lock manager
func NewLockManager() *LockManager {
	return &LockManager{
		locks: make(map[string]*FileLock),
	}
}

// AcquireLock acquires an in-memory lock for the given filepath
func (lm *LockManager) AcquireLock(filePath string, timeout time.Duration) (*FileLock, error) {
	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	// Check if lock already exists
	if existingLock, exists := lm.locks[filePath]; exists {
		// Check if lock is stale (older than timeout)
		if time.Since(existingLock.acquired) > timeout {
			// Remove stale lock
			delete(lm.locks, filePath)
		} else {
			return nil, fmt.Errorf("file is currently locked: %s", filePath)
		}
	}

	// Create new lock
	lock := &FileLock{
		filepath: filePath,
		acquired: time.Now(),
	}

	lm.locks[filePath] = lock
	return lock, nil
}

// ReleaseLock releases an in-memory file lock
func (lm *LockManager) ReleaseLock(lock *FileLock) error {
	if lock == nil {
		return nil
	}

	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	// Remove from manager
	delete(lm.locks, lock.filepath)
	return nil
}

// IsLocked checks if a file is currently locked
func (lm *LockManager) IsLocked(filePath string) bool {
	lm.mutex.RLock()
	defer lm.mutex.RUnlock()

	lock, exists := lm.locks[filePath]
	if !exists {
		return false
	}

	// Check if lock is stale
	if time.Since(lock.acquired) > 30*time.Second {
		// Lock is stale, remove it
		lm.mutex.RUnlock()
		lm.mutex.Lock()
		delete(lm.locks, filePath)
		lm.mutex.Unlock()
		lm.mutex.RLock()
		return false
	}

	return true
}

// CleanupStaleLocks removes all stale locks
func (lm *LockManager) CleanupStaleLocks(timeout time.Duration) {
	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	now := time.Now()
	for filePath, lock := range lm.locks {
		if now.Sub(lock.acquired) > timeout {
			delete(lm.locks, filePath)
		}
	}
}
