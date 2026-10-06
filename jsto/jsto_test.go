package jsto_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/transporter/v2"
	"github.com/seanmmitchell/transporter/v2/jsto"
)

// testLogger returns a log engine that forwards every message to t.Log and
// records it for inspection.
func testLogger(t *testing.T) (*ale.LogEngine, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var messages []string
	le := ale.CreateLogEngine("test")
	le.AddLogPipeline(ale.Debug, func(l *ale.Log) error {
		mu.Lock()
		messages = append(messages, l.Message)
		mu.Unlock()
		t.Log(l.String())
		return nil
	})
	return le, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), messages...)
	}
}

func quietLogger() *ale.LogEngine {
	return ale.CreateLogEngine("test")
}

func samplePattern(value string) *transporter.Pattern {
	return &transporter.Pattern{Sequences: map[string]transporter.PatternSequence{
		"token": {Name: "Token", Value: value, ENVVars: []string{"TOKEN"}},
		"debug": {Name: "Debug", Value: "no persistence", DisablePersistence: true},
	}}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestLoadMissingFileWrapsErrNotExist(t *testing.T) {
	conf := jsto.New(filepath.Join(t.TempDir(), "missing.json"))
	data, err := conf.Load(quietLogger())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load error = %v, want one wrapping fs.ErrNotExist", err)
	}
	if data != nil {
		t.Fatalf("Load data = %v, want nil", data)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	// encoding/json syntax errors quote the offending byte
	// ("invalid character 'Z' in literal true"), so 'Z' is a canary for file
	// content leaking into logs or returned errors.
	const canary = "'Z'"
	for name, contents := range map[string]string{
		"bad literal": `{"token": {"Value": tZ}}`,
		"bad start":   `Z13371337`,
		"truncated":   `{"token": {"Value": "Z`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conf.json")
			writeFile(t, path, contents)
			le, logs := testLogger(t)

			_, err := jsto.New(path).Load(le)
			if err == nil {
				t.Fatal("Load of corrupt JSON succeeded")
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Load error %v must not match fs.ErrNotExist", err)
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("file contents leaked into the returned error: %v", err)
			}
			for _, msg := range logs() {
				if strings.Contains(msg, canary) {
					t.Fatalf("file contents leaked into log: %q", msg)
				}
			}
		})
	}
}

func TestLoadNonObjectJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	writeFile(t, path, `["a", "b"]`)
	if _, err := jsto.New(path).Load(quietLogger()); err == nil {
		t.Fatal("Load of a JSON array succeeded")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	for name, contents := range map[string]string{
		"empty":      "",
		"whitespace": " \n\t\r\n ",
		"null":       "null",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conf.json")
			writeFile(t, path, contents)
			data, err := jsto.New(path).Load(quietLogger())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if data == nil || len(data) != 0 {
				t.Fatalf("Load data = %#v, want empty non-nil map", data)
			}
		})
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	le, logs := testLogger(t)
	conf := jsto.New(path)

	if err := conf.Save(le, samplePattern("s3cr3t-value")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := conf.Load(le)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	token, ok := data["token"].(map[string]interface{})
	if !ok {
		t.Fatalf("token entry = %#v, want object", data["token"])
	}
	if token["Value"] != "s3cr3t-value" {
		t.Errorf("token Value = %#v, want %q", token["Value"], "s3cr3t-value")
	}
	if token["DisablePersistence"] != false {
		t.Errorf("token DisablePersistence = %#v, want false", token["DisablePersistence"])
	}
	if _, present := token["ENVVars"]; present {
		t.Errorf("ENVVars should not be persisted")
	}

	debug, ok := data["debug"].(map[string]interface{})
	if !ok {
		t.Fatalf("debug entry = %#v, want object", data["debug"])
	}
	if debug["DisablePersistence"] != true {
		t.Errorf("debug DisablePersistence = %#v, want true", debug["DisablePersistence"])
	}

	for _, msg := range logs() {
		if strings.Contains(msg, "s3cr3t-value") {
			t.Fatalf("value leaked into log: %q", msg)
		}
	}
}

func TestSaveOverwritesWithShorterContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	conf := jsto.New(path)
	if err := conf.Save(quietLogger(), samplePattern(strings.Repeat("x", 4096))); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := conf.Save(quietLogger(), samplePattern("short")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := conf.Load(quietLogger())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := data["token"].(map[string]interface{})["Value"]; got != "short" {
		t.Fatalf("Value = %#v, want %q", got, "short")
	}
}

func TestSaveFileModeIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on windows")
	}

	assertMode := func(t *testing.T, path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file mode = %v, want 0600", perm)
		}
	}

	t.Run("new file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf.json")
		if err := jsto.New(path).Save(quietLogger(), samplePattern("v")); err != nil {
			t.Fatalf("Save: %v", err)
		}
		assertMode(t, path)
	})

	t.Run("existing 0644 file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "conf.json")
		writeFile(t, path, "{}")
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := jsto.New(path).Save(quietLogger(), samplePattern("v")); err != nil {
			t.Fatalf("Save: %v", err)
		}
		assertMode(t, path)
	})
}

func TestSaveThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs extra privileges on windows")
	}
	targetDir, linkDir := t.TempDir(), t.TempDir()
	target := filepath.Join(targetDir, "real.json")
	link := filepath.Join(linkDir, "conf.json")
	writeFile(t, target, "{}")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := jsto.New(link).Save(quietLogger(), samplePattern("via-link")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("symlink was replaced (info=%v, err=%v)", info, err)
	}
	data, err := jsto.New(target).Load(quietLogger())
	if err != nil {
		t.Fatalf("Load target: %v", err)
	}
	if got := data["token"].(map[string]interface{})["Value"]; got != "via-link" {
		t.Fatalf("target Value = %v, want %q", got, "via-link")
	}
	assertNoTempFiles(t, targetDir)
	assertNoTempFiles(t, linkDir)
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	conf := jsto.New(filepath.Join(dir, "conf.json"))
	for i := 0; i < 3; i++ {
		if err := conf.Save(quietLogger(), samplePattern(fmt.Sprint(i))); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	assertNoTempFiles(t, dir)
}

func TestSaveFailureCleansUpTempFile(t *testing.T) {
	dir := t.TempDir()
	// Renaming a regular file over a directory fails, exercising the cleanup
	// path after the temporary file has been written.
	target := filepath.Join(dir, "conf.json")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := jsto.New(target).Save(quietLogger(), samplePattern("v")); err == nil {
		t.Fatal("Save over a directory succeeded")
	}
	assertNoTempFiles(t, dir)
}

func TestSaveNilPattern(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	if err := jsto.New(path).Save(quietLogger(), nil); err == nil {
		t.Fatal("Save(nil) succeeded")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Save(nil) created a file (stat err = %v)", err)
	}
}

func TestEmptyFilePath(t *testing.T) {
	conf := &jsto.JSONConfig{}
	if _, err := conf.Load(quietLogger()); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load with empty FilePath: err = %v, want a non-ErrNotExist error", err)
	}
	if err := conf.Save(quietLogger(), samplePattern("v")); err == nil {
		t.Fatal("Save with empty FilePath succeeded")
	}
}

func TestNilLogEngine(t *testing.T) {
	conf := jsto.New(filepath.Join(t.TempDir(), "conf.json"))
	if err := conf.Save(nil, samplePattern("v")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := conf.Load(nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestZeroValueJSONConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	conf := &jsto.JSONConfig{FilePath: path}
	if _, err := conf.Load(quietLogger()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load error = %v, want fs.ErrNotExist", err)
	}
	if err := conf.Save(quietLogger(), samplePattern("v")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := conf.Load(quietLogger()); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestConcurrentSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.json")
	le, _ := testLogger(t)
	conf := jsto.New(path)
	if err := conf.Save(le, samplePattern("initial")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	const workers, iterations = 8, 25
	var wg sync.WaitGroup
	errs := make(chan error, workers*iterations*2)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := conf.Save(le, samplePattern(fmt.Sprintf("w%d-i%d", w, i))); err != nil {
					errs <- fmt.Errorf("Save: %w", err)
				}
				data, err := conf.Load(le)
				if err != nil {
					errs <- fmt.Errorf("Load: %w", err)
					continue
				}
				if _, ok := data["token"]; !ok {
					errs <- fmt.Errorf("Load returned data without token: %v", data)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentInstancesSeeWholeFiles uses separate JSONConfig values (and so
// separate mutexes) for one path: only the atomic rename keeps readers from
// observing a truncated or partially written file.
func TestConcurrentInstancesSeeWholeFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows may refuse to replace a file another handle has open")
	}
	path := filepath.Join(t.TempDir(), "conf.json")
	if err := jsto.New(path).Save(quietLogger(), samplePattern("initial")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	big := strings.Repeat("x", 64*1024)
	var wg sync.WaitGroup
	errs := make(chan error, 400)
	for w := 0; w < 2; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			writer := jsto.New(path)
			for i := 0; i < 50; i++ {
				if err := writer.Save(quietLogger(), samplePattern(big)); err != nil {
					errs <- fmt.Errorf("Save: %w", err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			reader := jsto.New(path)
			for i := 0; i < 50; i++ {
				data, err := reader.Load(quietLogger())
				if err != nil {
					errs <- fmt.Errorf("Load: %w", err)
				} else if _, ok := data["token"]; !ok {
					// An empty (truncated) file loads as {} without error.
					errs <- fmt.Errorf("Load saw a file without %q: %v", "token", data)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestLoadDoesNotLeakFileDescriptors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fd counting uses /proc/self/fd")
	}
	countFDs := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}

	path := filepath.Join(t.TempDir(), "conf.json")
	conf := jsto.New(path)
	if err := conf.Save(quietLogger(), samplePattern("v")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	before := countFDs()
	for i := 0; i < 100; i++ {
		if _, err := conf.Load(quietLogger()); err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	if after := countFDs(); after-before > 10 {
		t.Fatalf("open fds grew from %d to %d across 100 Loads", before, after)
	}
}
