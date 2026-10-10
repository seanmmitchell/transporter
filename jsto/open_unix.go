//go:build unix

package jsto

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// leaseWait bounds how long openForRead retries while another process holds a
// lease on the file; the kernel breaks a lease within lease-break-time (45s by
// default). A var so tests could lower it.
var leaseWait = 60 * time.Second

// openForRead opens path for reading without blocking on a FIFO, so
// readRegularFile can inspect the file and refuse it.
//
// A non-blocking open of a regular file under a write lease (e.g. a Samba
// oplock) fails with EWOULDBLOCK instead of waiting, although the kernel
// starts breaking the lease on that first attempt; retry until the lease is
// gone. A non-blocking read-only open of a FIFO never fails this way, so the
// retry cannot reintroduce blocking on a FIFO.
func openForRead(path string) (*os.File, error) {
	deadline := time.Now().Add(leaseWait)
	backoff := 5 * time.Millisecond
	for {
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err == nil || !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return f, err
		}
		time.Sleep(backoff)
		if backoff < time.Second {
			backoff *= 2
		}
	}
}

// setBlocking clears the O_NONBLOCK flag openForRead set, once f is known to
// be a regular file, so reads keep normal blocking semantics.
func setBlocking(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if err := rc.Control(func(fd uintptr) {
		setErr = syscall.SetNonblock(int(fd), false)
	}); err != nil {
		return err
	}
	return setErr
}
