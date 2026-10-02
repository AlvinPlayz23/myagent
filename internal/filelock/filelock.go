// Package filelock serializes read-modify-write operations across processes.
// Locks use a persistent sidecar, not the data file which atomic saves replace.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

const DefaultTimeout = 5 * time.Second

// ErrTimeout reports that another writer held the lock for too long.
var ErrTimeout = errors.New("file lock acquisition timed out")

// Lock owns an OS advisory lock. Closing it releases the lock and its handle.
type Lock struct {
	file *os.File
	once sync.Once
	err  error
}

// Acquire locks path + ".lock" exclusively, waiting at most DefaultTimeout.
// The parent directory must exist. The sidecar must never be removed, even
// after Close: deleting a held lock file breaks mutual exclusion on Unix.
func Acquire(path string) (*Lock, error) {
	return acquire(path, DefaultTimeout)
}

func acquire(path string, timeout time.Duration) (*Lock, error) {
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("filelock: open: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		locked, err := tryLock(file)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("filelock: acquire: %w", err), file.Close())
		}
		if locked {
			return &Lock{file: file}, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errors.Join(ErrTimeout, file.Close())
		}
		time.Sleep(min(10*time.Millisecond, remaining))
	}
}

// Close releases the lock without deleting the sidecar. It is idempotent.
// The operating system also releases the lock if the process exits or crashes.
func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		l.err = errors.Join(unlock(l.file), l.file.Close())
	})
	return l.err
}
