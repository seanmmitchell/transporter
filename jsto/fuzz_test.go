package jsto_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/seanmmitchell/transporter/v2/jsto"
)

// jsonErrorWording matches encoding/json error text, which quotes input.
var jsonErrorWording = regexp.MustCompile(`invalid character|cannot unmarshal|unexpected end of JSON`)

// sanitizedParseError matches every error Load returns for bad JSON at path;
// none of these forms can carry file content.
func sanitizedParseError(path string) *regexp.Regexp {
	return regexp.MustCompile(`^jsto: parsing ` + regexp.QuoteMeta(strconv.Quote(path)) + `: jsto: invalid JSON: ` +
		`(invalid JSON at byte offset \d+|top-level JSON value must be an object|unsupported value at byte offset \d+|invalid JSON)$`)
}

// FuzzLoad feeds arbitrary bytes to Load. Invariants: exactly one of data or
// error; errors wrap ErrInvalidJSON (or ErrFileTooLarge past 16 MiB); Load
// agrees with encoding/json on what is valid; and file content (marked with
// "Zq9") never reaches the returned error or the logs.
func FuzzLoad(f *testing.F) {
	f.Add([]byte(`{"token": {"Value": "Zq9"}}`))
	f.Add([]byte(`{"token": {"Value": tZq9}}`))
	f.Add([]byte(`Zq9`))
	f.Add([]byte(`{"x": {"Value": 1e999}}`))
	f.Add([]byte(""))
	f.Add([]byte(" \n\t\r"))
	f.Add([]byte("\u00a0")) // Unicode space: not JSON whitespace, so invalid
	f.Add([]byte("\v\f"))
	f.Add([]byte(`null`))
	f.Add([]byte(`[1, 2]`))

	// One file per fuzzing process, rewritten each run: a fresh t.TempDir per
	// input made each execution slow.
	path := filepath.Join(f.TempDir(), "conf.json")
	wantParseError := sanitizedParseError(path)

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		le, logs := testLogger(t)
		got, err := jsto.New(path).Load(le)

		if (err == nil) == (got == nil) {
			t.Fatalf("Load returned data %v and error %v; want exactly one", got, err)
		}
		if len(data) > 16<<20 {
			if !errors.Is(err, jsto.ErrFileTooLarge) {
				t.Fatalf("Load of %d bytes: err = %v, want ErrFileTooLarge", len(data), err)
			}
			return
		}

		var m map[string]interface{}
		jsonErr := json.Unmarshal(data, &m)
		blank := len(bytes.Trim(data, " \t\r\n")) == 0

		if err != nil {
			if !errors.Is(err, jsto.ErrInvalidJSON) {
				t.Fatalf("Load error %v does not wrap ErrInvalidJSON", err)
			}
			if blank || jsonErr == nil {
				t.Fatalf("Load rejected input encoding/json accepts: %v", err)
			}
			// encoding/json's own messages quote input bytes or number
			// literals, so the error must be one of the sanitized forms.
			if !wantParseError.MatchString(err.Error()) || strings.Contains(err.Error(), "Zq9") {
				t.Fatalf("parse error is not one of the sanitized forms (may carry file content): %v", err)
			}
		} else if !blank && jsonErr != nil {
			t.Fatalf("Load accepted input encoding/json rejects (%v)", jsonErr)
		}

		for _, msg := range logs() {
			if strings.Contains(msg, "Zq9") || jsonErrorWording.MatchString(msg) {
				t.Fatalf("file content may have leaked into a log message: %q", msg)
			}
		}
	})
}
