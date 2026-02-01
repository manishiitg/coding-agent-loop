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

// AcquireLock acquires an in-memory exclusive lock for the given filepath (for write operations)
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

// AcquireReadLock acquires a read lock (conceptually, currently checking if write locked)
// Since we don't support shared read locks in the map structure yet (exclusive only),
// this mainly checks if a Write lock is held and waits/fails.
// For now, we'll implement a non-blocking check that fails if Write locked.
func (lm *LockManager) AcquireReadLock(filePath string, timeout time.Duration) error {
	lm.mutex.RLock()
	defer lm.mutex.RUnlock()

	if existingLock, exists := lm.locks[filePath]; exists {
		if time.Since(existingLock.acquired) <= timeout {
			return fmt.Errorf("file is currently locked for writing: %s", filePath)
		}
	}
	return nil
}

// ReleaseLock releases an in-memory file lock
func (lm *LockManager) ReleaseLock(lock *FileLock) error {
	if lock == nil {
		return nil
	}

	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	// Only remove if it's the same lock (prevent removing a newer lock)
	if existing, ok := lm.locks[lock.filepath]; ok && existing == lock {
		delete(lm.locks, lock.filepath)
	}
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
