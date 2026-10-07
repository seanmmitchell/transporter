package jsto

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func setLease(t *testing.T, f *os.File, kind int) error {
	t.Helper()
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_SETLEASE, uintptr(kind))
	if errno != 0 {
		return errno
	}
	return nil
}

// Once a file is known to be regular, reads must not run with O_NONBLOCK.
func TestSetBlockingClearsNonblock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openForRead(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := setBlocking(f); err != nil {
		t.Fatalf("setBlocking: %v", err)
	}

	rc, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags uintptr
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
	}); err != nil || errno != 0 {
		t.Fatalf("F_GETFL: %v %v", err, errno)
	}
	if flags&syscall.O_NONBLOCK != 0 {
		t.Fatal("O_NONBLOCK still set after setBlocking")
	}
}

// A non-blocking open of a file under a write lease (e.g. a Samba oplock)
// fails with EWOULDBLOCK; openForRead must wait for the lease to be released
// instead of failing Load at once.
func TestOpenForReadWaitsForLeaseBreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	holder, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := setLease(t, holder, syscall.F_WRLCK); err != nil {
		t.Skipf("file leases unavailable: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		f, err := openForRead(path)
		if f != nil {
			f.Close()
		}
		done <- err
	}()

	// Release the lease the way a holder does when told it is being broken.
	time.Sleep(100 * time.Millisecond)
	if err := setLease(t, holder, syscall.F_UNLCK); err != nil {
		t.Fatalf("releasing lease: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("openForRead under a lease: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("openForRead did not return after the lease was released")
	}
}
