//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package jsto

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Load's os.Stat pre-check can be raced (a FIFO swapped in before the open),
// so readRegularFile must neither block on a FIFO nor read one.
func TestReadRegularFileRefusesFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readRegularFile(path)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegularFile) {
			t.Fatalf("readRegularFile(FIFO): err = %v, want ErrNotRegularFile", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("readRegularFile blocked opening a FIFO")
	}
}
