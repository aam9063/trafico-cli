//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// acquireFileLock takes an exclusive byte-range lock on the first byte of
// the open file without blocking: it fails immediately when another handle
// (in this or any other process) already locks the region. The kernel
// releases the lock when the holder dies, so no staleness heuristic is
// needed.
func acquireFileLock(file *os.File) error {
	overlapped := new(windows.Overlapped) // zero: lock byte 0
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped)
}

// releaseFileLock releases the byte-range lock taken by acquireFileLock:
// the same region (1 byte at offset 0), through the same handle.
func releaseFileLock(file *os.File) error {
	overlapped := new(windows.Overlapped) // zero: unlock byte 0
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
}
