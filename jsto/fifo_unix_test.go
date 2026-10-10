//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

// (syscall.Mkfifo exists only on these; plain "unix" also covers solaris and aix.)

package jsto_test

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/seanmmitchell/transporter/v2/jsto"
)

// A FIFO at the config path used to block Load (and so Energize) forever.
func TestLoadRefusesFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := jsto.New(path).Load(quietLogger())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, jsto.ErrNotRegularFile) {
			t.Fatalf("Load(FIFO): err = %v, want ErrNotRegularFile", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
}
