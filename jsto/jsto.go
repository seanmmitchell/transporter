package jsto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/transporter/v2"
)

var (
	errEmptyPath  = errors.New("jsto: FilePath is empty")
	errNilPattern = errors.New("jsto: cannot save a nil pattern")
)

// JSONConfig stores a pattern as a JSON file. The zero value is usable once
// FilePath is set; it must not be copied after first use.
type JSONConfig struct {
	FilePath string

	mu sync.Mutex
}

// New returns a JSONConfig backed by the file at path.
func New(path string) *JSONConfig {
	return &JSONConfig{FilePath: path}
}

// Load reads and decodes the JSON file at FilePath. If the file does not exist
// the returned error wraps fs.ErrNotExist. An empty or whitespace-only file
// yields an empty map. File contents are never logged.
func (conf *JSONConfig) Load(le *ale.LogEngine) (map[string]interface{}, error) {
	conf.mu.Lock()
	defer conf.mu.Unlock()
	le = logEngineOrDiscard(le)
	path := conf.FilePath
	le.Log(ale.Info, "Loading JSON File...")

	if path == "" {
		le.Log(ale.Error, "\t==> No JSON file path configured.")
		return nil, errEmptyPath
	}

	le.Log(ale.Verbose, "Reading JSON File...")
	allBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			le.Log(ale.Info, fmt.Sprintf("JSON File %q does not exist.", path))
		} else {
			le.Log(ale.Error, fmt.Sprintf("\t==> Failed to read JSON file. Error: %q", err))
		}
		return nil, fmt.Errorf("jsto: reading %q: %w", path, err)
	}
	le.Log(ale.Verbose, "JSON File Read.")

	if len(bytes.TrimSpace(allBytes)) == 0 {
		le.Log(ale.Info, "JSON File is empty.")
		return map[string]interface{}{}, nil
	}

	le.Log(ale.Verbose, "Unmarshaling JSON File...")
	var jsonData map[string]interface{}
	err = json.Unmarshal(allBytes, &jsonData)
	if err != nil {
		le.Log(ale.Error, fmt.Sprintf("\t==> Failed to unmarshal JSON file %q.", path))
		return nil, fmt.Errorf("jsto: parsing %q: %s", path, describeJSONError(err))
	}
	if jsonData == nil {
		// The file held a JSON null.
		jsonData = map[string]interface{}{}
	}
	le.Log(ale.Verbose, "JSON File Unmarshaled.")

	le.Log(ale.Info, "JSON File Loaded.")
	return jsonData, nil
}

// Save writes the pattern's sequences to FilePath as indented JSON. The data
// goes to a private (0600) temporary file in the same directory which is then
// renamed over FilePath, so readers never observe a partially written file.
func (conf *JSONConfig) Save(le *ale.LogEngine, pattern *transporter.Pattern) error {
	conf.mu.Lock()
	defer conf.mu.Unlock()
	le = logEngineOrDiscard(le)
	path := conf.FilePath
	le.Log(ale.Info, "Saving JSON File...")

	if path == "" {
		le.Log(ale.Error, "\t==> No JSON file path configured.")
		return errEmptyPath
	}
	if pattern == nil {
		le.Log(ale.Error, "\t==> Cannot save a nil pattern.")
		return errNilPattern
	}

	le.Log(ale.Verbose, "Marshaling Pattern...")
	data, err := json.MarshalIndent(pattern.Sequences, "", "\t")
	if err != nil {
		le.Log(ale.Error, "\t==> Failed to marshal pattern.")
		return fmt.Errorf("jsto: encoding pattern: %w", err)
	}
	le.Log(ale.Verbose, "Pattern Marshaled.")

	// Write through a symlink so the link itself survives the rename.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	le.Log(ale.Verbose, "Writing JSON File...")
	err = writeFileAtomic(path, data)
	if err != nil {
		le.Log(ale.Error, fmt.Sprintf("\t==> Failed to write JSON file. Error: %q", err))
		return err
	}

	le.Log(ale.Info, "JSON File Saved.")
	return nil
}

// writeFileAtomic writes data to a 0600 temporary file beside path, flushes it
// to disk and renames it over path. The temporary file is removed on failure.
func writeFileAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("jsto: creating temp file for %q: %w", path, err)
	}
	tmpPath := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmpPath)
		}
	}()

	// os.CreateTemp already creates the file with mode 0600.
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("jsto: writing %q: %w", tmpPath, err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("jsto: syncing %q: %w", tmpPath, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("jsto: closing %q: %w", tmpPath, err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("jsto: replacing %q: %w", path, err)
	}
	return nil
}

// describeJSONError summarizes a decode failure without quoting file content,
// which encoding/json errors do (e.g. "invalid character 'x' in literal true").
func describeJSONError(err error) string {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Sprintf("invalid JSON at byte offset %d", syntaxErr.Offset)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "top-level JSON value must be an object"
	}
	return "invalid JSON"
}

// logEngineOrDiscard substitutes a pipeline-less engine for a nil one so that
// callers may omit logging.
func logEngineOrDiscard(le *ale.LogEngine) *ale.LogEngine {
	if le == nil {
		return ale.CreateLogEngine("jsto")
	}
	return le
}
