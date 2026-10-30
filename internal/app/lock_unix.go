//go:build !windows

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// acquireFileLock takes an exclusive advisory lock on the open file without
// blocking: it fails immediately when another process (or another open file
// description of this process) already holds the lock. The kernel releases
// the lock when the holder dies, so no staleness heuristic is needed.
func acquireFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

// releaseFileLock releases the advisory lock taken by acquireFileLock.
func releaseFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
