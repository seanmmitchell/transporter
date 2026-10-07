package jsto

import (
	"os"
	"path/filepath"
	"strings"
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

// Files such as /proc/self/pagemap report as regular with size 0 but never
// end, so Load must stop reading at maxConfigBytes.
func TestLoadCapsReadSize(t *testing.T) {
	orig := maxConfigBytes
	maxConfigBytes = 8
	t.Cleanup(func() { maxConfigBytes = orig })

	path := filepath.Join(t.TempDir(), "conf.json")
	if err := os.WriteFile(path, []byte(`{"a": {"Value": "12345"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := New(path).Load(nil)
	if err == nil || !strings.Contains(err.Error(), "larger than 8 bytes") {
		t.Fatalf("Load of a file over the cap: data %v, err %v; want a size error", data, err)
	}
}
