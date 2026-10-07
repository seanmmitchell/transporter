//go:build unix

package jsto

import (
	"os"
	"syscall"
)

// openForRead opens path for reading without blocking on a FIFO, so
// readRegularFile can inspect the file and refuse it.
func openForRead(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
