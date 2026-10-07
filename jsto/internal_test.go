package jsto

import (
	"os"
	"path/filepath"
	"testing"
)

// Save refuses a directory before writing, so the cleanup in writeFileAtomic
// is exercised here directly: renaming the temp file over a directory fails
// after the temp file was written.
func TestWriteFileAtomicRemovesTempFileOnFailure(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "conf.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory cannot be replaced by rename on any platform.
	if err := os.WriteFile(filepath.Join(target, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomic(target, []byte("{}")); err == nil {
		t.Fatal("writeFileAtomic over a directory succeeded")
	}

	leftovers, err := filepath.Glob(filepath.Join(parent, "*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}
