package transporter_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/transporter/v2"
	"github.com/seanmmitchell/transporter/v2/jsto"
)

// #region Helpers

const (
	keyFirst = "user-firstName"
	keyLast  = "user-lastName" // DisablePersistence
	keyAge   = "user-age"
)

// newPattern returns a fresh copy of the pattern most tests use.
func newPattern() transporter.Pattern {
	return transporter.Pattern{Sequences: map[string]transporter.PatternSequence{
		keyFirst: {
			Name:        "User's First Name",
			Description: "A variable for holding the user's first name.",
			CLIFlags:    []string{"f", "fn"},
			ENVVars:     []string{"FN"},
		},
		keyLast: {
			Name:               "User's Last Name",
			Description:        "A variable for holding the user's last name.",
			CLIFlags:           []string{"l", "ln"},
			ENVVars:            []string{"LN"},
			DisablePersistence: true,
		},
		keyAge: {
			Name:        "User's Age",
			Description: "A variable for holding the user's age.",
			CLIFlags:    []string{"a", "age"},
			ENVVars:     []string{"AGE"},
		},
	}}
}

// logCapture records every message logged to its engine. Safe for concurrent use.
type logCapture struct {
	mu   sync.Mutex
	logs []ale.Log
}

func newCaptureEngine() (*ale.LogEngine, *logCapture) {
	c := &logCapture{}
	le := ale.CreateLogEngine("transporter-test")
	le.AddLogPipeline(ale.Debug, c.record)
	return le, c
}

func (c *logCapture) record(l *ale.Log) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, *l)
	return nil
}

func (c *logCapture) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	msgs := make([]string, len(c.logs))
	for i, l := range c.logs {
		msgs[i] = l.Message
	}
	return msgs
}

// stubConfig is an in-memory ConfigFileInterface.
type stubConfig struct {
	mu       sync.Mutex
	loadData map[string]any // nil means "no file": Load returns an error wrapping fs.ErrNotExist
	loadErr  error
	saveErr  error

	loadLE *ale.LogEngine
	saveLE *ale.LogEngine
}

func (s *stubConfig) Load(le *ale.LogEngine) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLE = le
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.loadData == nil {
		return nil, fmt.Errorf("stub config: %w", fs.ErrNotExist)
	}
	return s.loadData, nil
}

func (s *stubConfig) Save(le *ale.LogEngine, p *transporter.Pattern) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveLE = le
	return s.saveErr
}

// baseOptions isolates Energize from the real process (no args, no environment,
// no config file) and captures all logs at Debug.
func baseOptions() (transporter.TransporterOptions, *logCapture) {
	le, logs := newCaptureEngine()
	return transporter.TransporterOptions{
		LogEngine:         le,
		EnvironmentPrefix: transporter.DefaultEnvironmentPrefix,
		Args:              []string{},
		Environ:           []string{},
	}, logs
}

func energize(t *testing.T, p transporter.Pattern, o transporter.TransporterOptions) *transporter.State {
	t.Helper()
	s, err := transporter.Energize(p, o)
	if err != nil {
		t.Fatalf("Energize: unexpected error: %v", err)
	}
	return s
}

func wantValue(t *testing.T, s *transporter.State, key, want string) {
	t.Helper()
	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get(%q): unexpected error: %v", key, err)
	}
	if got != want {
		t.Errorf("Get(%q) = %q, want %q", key, got, want)
	}
}

// wantAll checks every key of newPattern; keys absent from want must be empty.
func wantAll(t *testing.T, s *transporter.State, want map[string]string) {
	t.Helper()
	for _, key := range []string{keyFirst, keyLast, keyAge} {
		wantValue(t, s, key, want[key])
	}
}

func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", what, r)
		}
	}()
	f()
}

func readConfigFile(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg map[string]map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decoding config file: %v\n%s", err, data)
	}
	return cfg
}

func writeConfigFile(t *testing.T, path string, cfg map[string]map[string]any) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("encoding config file: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
}

// #endregion Helpers

func TestLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	o, _ := baseOptions()
	o.ConfigFileEngine = jsto.New(path)

	// CLI values, no config file yet.
	o.Args = []string{"--f", "sean", "--ln", "test", "--age", "21"}
	s := energize(t, newPattern(), o)
	wantAll(t, s, map[string]string{keyFirst: "sean", keyLast: "test", keyAge: "21"})
	if err := s.Materialize(); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Edit the file out of band.
	cfg := readConfigFile(t, path)
	if got := cfg[keyLast]["Value"]; got != transporter.CONF_DisablePersistence_Phrase {
		t.Errorf("saved %s Value = %v, want %q", keyLast, got, transporter.CONF_DisablePersistence_Phrase)
	}
	if cfg[keyAge] == nil {
		t.Fatalf("saved config has no %q entry: %v", keyAge, cfg)
	}
	cfg[keyAge]["Value"] = "25"
	writeConfigFile(t, path, cfg)

	// Reload from the file alone.
	o.Args = []string{}
	s = energize(t, newPattern(), o)
	wantAll(t, s, map[string]string{keyFirst: "sean", keyAge: "25"})

	// Set, persist, reload.
	if err := s.Set(keyFirst, "sue"); err != nil {
		t.Fatalf("Set(%q): %v", keyFirst, err)
	}
	if err := s.Materialize(); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	s = energize(t, newPattern(), o)
	wantAll(t, s, map[string]string{keyFirst: "sue", keyAge: "25"})

	// Environment on top of the file.
	o.Environ = []string{"T_LN=TESTLASTNAME"}
	s = energize(t, newPattern(), o)
	wantAll(t, s, map[string]string{keyFirst: "sue", keyLast: "TESTLASTNAME", keyAge: "25"})
}

func TestSourcePrecedence(t *testing.T) {
	cases := []struct {
		name    string
		environ []string
		args    []string
		want    string
	}{
		{"file only", nil, nil, "file"},
		{"env beats file", []string{"T_FN=env"}, nil, "env"},
		{"CLI beats env and file", []string{"T_FN=env"}, []string{"--f", "cli"}, "cli"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := baseOptions()
			o.ConfigFileEngine = &stubConfig{loadData: map[string]any{
				keyFirst: map[string]any{"DisablePersistence": false, "Value": "file"},
			}}
			o.Environ = append(o.Environ, tc.environ...)
			o.Args = append(o.Args, tc.args...)
			wantValue(t, energize(t, newPattern(), o), keyFirst, tc.want)
		})
	}
}

// #region Regression tests

func TestMaterializeKeepsInMemoryValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	o, _ := baseOptions()
	o.ConfigFileEngine = jsto.New(path)
	o.Args = []string{"--ln", "secret-last", "--f", "sean"}
	s := energize(t, newPattern(), o)

	// Twice: a second save must not observe a value redacted by the first.
	for i := 0; i < 2; i++ {
		if err := s.Materialize(); err != nil {
			t.Fatalf("Materialize #%d: %v", i+1, err)
		}
	}

	wantValue(t, s, keyLast, "secret-last")
	cfg := readConfigFile(t, path)
	if got := cfg[keyLast]["Value"]; got != transporter.CONF_DisablePersistence_Phrase {
		t.Errorf("saved %s Value = %v, want %q", keyLast, got, transporter.CONF_DisablePersistence_Phrase)
	}
	if got := cfg[keyFirst]["Value"]; got != "sean" {
		t.Errorf("saved %s Value = %v, want %q", keyFirst, got, "sean")
	}
}

func TestMaterializeWithoutEngine(t *testing.T) {
	o, _ := baseOptions()
	s := energize(t, newPattern(), o)
	var err error
	mustNotPanic(t, "Materialize without a ConfigFileEngine", func() { err = s.Materialize() })
	if !errors.Is(err, transporter.ErrNoConfigFileEngine) {
		t.Errorf("Materialize: err = %v, want ErrNoConfigFileEngine", err)
	}
}

func TestConfigFileLogEngineOnly(t *testing.T) {
	cfgLE, _ := newCaptureEngine()
	stub := &stubConfig{loadData: map[string]any{
		keyAge: map[string]any{"DisablePersistence": false, "Value": "40"},
	}}
	o := transporter.TransporterOptions{ // LogEngine deliberately nil
		ConfigFileLogEngine: cfgLE,
		ConfigFileEngine:    stub,
		EnvironmentPrefix:   transporter.DefaultEnvironmentPrefix,
		Args:                []string{},
		Environ:             []string{},
	}
	mustNotPanic(t, "Energize/Set/Materialize with only ConfigFileLogEngine", func() {
		s := energize(t, newPattern(), o)
		wantValue(t, s, keyAge, "40")
		if err := s.Set(keyFirst, "x"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := s.Materialize(); err != nil {
			t.Fatalf("Materialize: %v", err)
		}
	})
	if stub.loadLE != cfgLE {
		t.Errorf("Load did not receive ConfigFileLogEngine")
	}
	if stub.saveLE != cfgLE {
		t.Errorf("Save did not receive ConfigFileLogEngine")
	}
}

// overlapConfig records whether two Saves ever run at the same time.
type overlapConfig struct {
	active, overlapped atomic.Int32
}

func (c *overlapConfig) Load(*ale.LogEngine) (map[string]interface{}, error) {
	return nil, fs.ErrNotExist
}

func (c *overlapConfig) Save(*ale.LogEngine, *transporter.Pattern) error {
	if c.active.Add(1) > 1 {
		c.overlapped.Store(1)
	}
	time.Sleep(time.Millisecond)
	c.active.Add(-1)
	return nil
}

func TestMaterializeSerialized(t *testing.T) {
	cfg := &overlapConfig{}
	o, _ := baseOptions()
	o.ConfigFileEngine = cfg
	s := energize(t, newPattern(), o)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if err := s.Materialize(); err != nil {
					t.Errorf("Materialize: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	if cfg.overlapped.Load() != 0 {
		t.Errorf("concurrent Materialize calls overlapped in Save; an older snapshot could overwrite a newer one")
	}
}

func TestCorruptConfigFailsEnergize(t *testing.T) {
	t.Run("jsto garbage file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		garbage := []byte("{not json")
		if err := os.WriteFile(path, garbage, 0o600); err != nil {
			t.Fatalf("writing garbage file: %v", err)
		}
		o, _ := baseOptions()
		o.ConfigFileEngine = jsto.New(path)
		if _, err := transporter.Energize(newPattern(), o); err == nil {
			t.Errorf("Energize with a corrupt config file succeeded, want error")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading config file: %v", err)
		}
		if !bytes.Equal(got, garbage) {
			t.Errorf("config file changed: got %q, want %q", got, garbage)
		}
	})

	t.Run("stub load error", func(t *testing.T) {
		o, _ := baseOptions()
		o.ConfigFileEngine = &stubConfig{loadErr: errors.New("stub: permission denied")}
		if _, err := transporter.Energize(newPattern(), o); err == nil {
			t.Errorf("Energize with a failing Load succeeded, want error")
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		o, _ := baseOptions()
		o.ConfigFileEngine = &stubConfig{} // Load wraps fs.ErrNotExist
		energize(t, newPattern(), o)
	})
}

func TestSaveErrorPropagates(t *testing.T) {
	saveErr := errors.New("stub: disk full")
	o, _ := baseOptions()
	o.ConfigFileEngine = &stubConfig{saveErr: saveErr}
	s := energize(t, newPattern(), o)
	if err := s.Materialize(); !errors.Is(err, saveErr) {
		t.Errorf("Materialize: err = %v, want %v", err, saveErr)
	}
}

func TestEnvValueWithEquals(t *testing.T) {
	o, _ := baseOptions()
	o.Environ = []string{"T_FN=a=b"}
	wantValue(t, energize(t, newPattern(), o), keyFirst, "a=b")
}

func TestCLIForms(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want map[string]string // keys not listed must stay empty
	}{
		{"--flag value", []string{"--f", "v"}, map[string]string{keyFirst: "v"}},
		{"-flag value", []string{"-f", "v"}, map[string]string{keyFirst: "v"}},
		{"--flag=value", []string{"--f=v"}, map[string]string{keyFirst: "v"}},
		{"alias and sequence key", []string{"--fn", "a", "--user-age", "30"}, map[string]string{keyFirst: "a", keyAge: "30"}},
		{"-- ends parsing", []string{"--f", "x", "--", "--f", "v", "--age", "9"}, map[string]string{keyFirst: "x"}},
		{"-- is never a flag value", []string{"--age", "--", "--f", "v"}, map[string]string{}},
		{"value may start with -", []string{"--age", "-5"}, map[string]string{keyAge: "-5"}},
		{"lone - is positional", []string{"-", "--f", "v"}, map[string]string{keyFirst: "v"}},
		{"unknown flag does not consume next arg", []string{"--nope", "--f", "v"}, map[string]string{keyFirst: "v"}},
		{"explicit Args parsed from first element", []string{"--age", "99", "--f", "v"}, map[string]string{keyFirst: "v", keyAge: "99"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := baseOptions()
			o.Args = tc.args
			wantAll(t, energize(t, newPattern(), o), tc.want)
		})
	}

	t.Run("nil Args skips argv[0]", func(t *testing.T) {
		orig := os.Args
		t.Cleanup(func() { os.Args = orig })
		os.Args = []string{"--age", "99", "--f", "v"}

		o, _ := baseOptions()
		o.Args = nil
		wantAll(t, energize(t, newPattern(), o), map[string]string{keyFirst: "v"})
	})
}

func TestEmptyPrefixDefaultsToT(t *testing.T) {
	o, _ := baseOptions()
	o.EnvironmentPrefix = ""
	o.Environ = []string{"T_FN=prefixed", "FN=bare"}
	wantValue(t, energize(t, newPattern(), o), keyFirst, "prefixed")
}

func TestSourceIsolation(t *testing.T) {
	t.Run("CLI flag named like an ENV var", func(t *testing.T) {
		o, _ := baseOptions()
		o.Args = []string{"--FN", "cli", "--age", "30"}
		wantAll(t, energize(t, newPattern(), o), map[string]string{keyAge: "30"})
	})

	t.Run("ENV var named like a CLI flag", func(t *testing.T) {
		o, _ := baseOptions()
		o.Environ = []string{"T_fn=env", "T_f=env", "T_user-age=30"}
		wantAll(t, energize(t, newPattern(), o), map[string]string{keyAge: "30"})
	})
}

func TestDuplicateIdentifiers(t *testing.T) {
	type seqs = map[string]transporter.PatternSequence
	cases := []struct {
		name    string
		seqs    seqs
		wantDup bool
	}{
		{"shared CLI flag", seqs{"a": {CLIFlags: []string{"x"}}, "b": {CLIFlags: []string{"x"}}}, true},
		{"shared ENV var", seqs{"a": {ENVVars: []string{"X"}}, "b": {ENVVars: []string{"X"}}}, true},
		{"CLI flag equals another key", seqs{"a": {}, "b": {CLIFlags: []string{"a"}}}, true},
		{"ENV var equals another key", seqs{"a": {}, "b": {ENVVars: []string{"a"}}}, true},
		{"same name in CLI and ENV namespaces", seqs{"a": {CLIFlags: []string{"x"}}, "b": {ENVVars: []string{"x"}}}, false},
		{"repeats within one sequence", seqs{"a": {CLIFlags: []string{"a", "x"}, ENVVars: []string{"a", "x"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := baseOptions()
			_, err := transporter.Energize(transporter.Pattern{Sequences: tc.seqs}, o)
			if tc.wantDup && !errors.Is(err, transporter.ErrDuplicateIdentifier) {
				t.Errorf("Energize: err = %v, want ErrDuplicateIdentifier", err)
			}
			if !tc.wantDup && err != nil {
				t.Errorf("Energize: unexpected error: %v", err)
			}
		})
	}
}

func TestInvalidIdentifiers(t *testing.T) {
	type seqs = map[string]transporter.PatternSequence
	cases := map[string]seqs{
		"CLI flag with dashes":  {"a": {CLIFlags: []string{"--port"}}},
		"CLI flag with a dash":  {"a": {CLIFlags: []string{"-p"}}},
		"CLI flag containing =": {"a": {CLIFlags: []string{"p=1"}}},
		"ENV var containing =":  {"a": {ENVVars: []string{"P=1"}}},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			o, _ := baseOptions()
			_, err := transporter.Energize(transporter.Pattern{Sequences: s}, o)
			if !errors.Is(err, transporter.ErrInvalidIdentifier) {
				t.Errorf("Energize: err = %v, want ErrInvalidIdentifier", err)
			}
		})
	}
}

func TestCallerPatternNotMutated(t *testing.T) {
	p := newPattern()
	o, _ := baseOptions()
	o.ConfigFileEngine = &stubConfig{loadData: map[string]any{
		keyAge: map[string]any{"DisablePersistence": false, "Value": "50"},
	}}
	o.Args = []string{"--f", "cli"}
	o.Environ = []string{"T_LN=env"}

	s := energize(t, p, o)
	if err := s.Set(keyAge, "51"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Materialize(); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	for key, seq := range p.Sequences {
		if seq.Value != "" {
			t.Errorf("caller's pattern mutated: %s Value = %q, want empty", key, seq.Value)
		}
	}

	// Changes to the caller's map must not reach the state either.
	seq := p.Sequences[keyFirst]
	seq.Value = "mutated"
	p.Sequences[keyFirst] = seq
	delete(p.Sequences, keyAge)
	wantAll(t, s, map[string]string{keyFirst: "cli", keyLast: "env", keyAge: "51"})
}

func TestConfigEntryWithoutDisablePersistenceLoads(t *testing.T) {
	o, _ := baseOptions()
	o.ConfigFileEngine = &stubConfig{loadData: map[string]any{
		keyAge: map[string]any{"Value": "33"},
	}}
	wantValue(t, energize(t, newPattern(), o), keyAge, "33")
}

func TestFileCannotReenableDisabledPersistence(t *testing.T) {
	o, _ := baseOptions()
	o.ConfigFileEngine = &stubConfig{loadData: map[string]any{
		keyLast: map[string]any{"DisablePersistence": false, "Value": "from-file"},
		keyAge:  map[string]any{"DisablePersistence": false, "Value": "33"}, // control: file was read
	}}
	wantAll(t, energize(t, newPattern(), o), map[string]string{keyAge: "33"})
}

func TestConfigEntrySkipping(t *testing.T) {
	o, _ := baseOptions()
	o.ConfigFileEngine = &stubConfig{loadData: map[string]any{
		keyFirst: map[string]any{"DisablePersistence": true, "Value": "file-disabled"}, // file flag alone disables
		keyAge:   "not an object",
		"other":  map[string]any{"Value": "unknown key"},
	}}
	wantAll(t, energize(t, newPattern(), o), map[string]string{})

	o.ConfigFileEngine = &stubConfig{loadData: map[string]any{
		keyAge: map[string]any{"Value": 33}, // non-string Value
	}}
	wantAll(t, energize(t, newPattern(), o), map[string]string{})
}

func TestRequiredMissing(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		environ []string
		wantErr bool
	}{
		{"no source", nil, nil, true},
		{"empty CLI value", []string{"--age", ""}, nil, true},
		{"CLI", []string{"--age", "30"}, nil, false},
		{"env", nil, []string{"T_AGE=30"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPattern()
			age := p.Sequences[keyAge]
			age.Required = true
			p.Sequences[keyAge] = age

			o, _ := baseOptions()
			o.Args = append(o.Args, tc.args...)
			o.Environ = append(o.Environ, tc.environ...)
			s, err := transporter.Energize(p, o)
			if tc.wantErr {
				if !errors.Is(err, transporter.ErrRequiredMissing) {
					t.Errorf("Energize: err = %v, want ErrRequiredMissing", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Energize: unexpected error: %v", err)
			}
			wantValue(t, s, keyAge, "30")
		})
	}
}

func TestSecretsNeverLogged(t *testing.T) {
	const canary = "c4n4ry"
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfigFile(t, path, map[string]map[string]any{
		keyAge: {"DisablePersistence": false, "Value": canary + "-file"},
	})

	o, logs := baseOptions()
	o.ConfigFileEngine = jsto.New(path)
	o.DumpEnvironmentVariables = true
	o.DumpCLIArguments = true
	o.Environ = []string{"T_LN=" + canary + "-env", "UNRELATED=" + canary + "-unrelated"}
	// "-…-after-unknown" follows an unknown flag, so the parser sees it as
	// another unknown flag; it is still a value and must not be logged.
	o.Args = []string{"--f", canary + "-cli", "--age=" + canary + "-cli-eq", "--tokn", "-" + canary + "-after-unknown", canary + "-positional"}

	s := energize(t, newPattern(), o)
	wantAll(t, s, map[string]string{keyFirst: canary + "-cli", keyLast: canary + "-env", keyAge: canary + "-cli-eq"})
	if err := s.Set(keyFirst, canary+"-set"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Materialize(); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	msgs := logs.messages()
	if len(msgs) == 0 {
		t.Fatalf("no log messages captured; the leak check would be vacuous")
	}
	for _, m := range msgs {
		if strings.Contains(m, canary) {
			t.Errorf("log message leaks a value: %q", m)
		}
	}
}

func TestGetUnknownKey(t *testing.T) {
	o, _ := baseOptions()
	s := energize(t, newPattern(), o)
	if _, err := s.Get("missing"); !errors.Is(err, transporter.ErrKeyNotFound) {
		t.Errorf("Get(missing): err = %v, want ErrKeyNotFound", err)
	}
	if err := s.Set("missing", "x"); !errors.Is(err, transporter.ErrKeyNotFound) {
		t.Errorf("Set(missing): err = %v, want ErrKeyNotFound", err)
	}
}

// TestConcurrentGetSet stays last: against a non-copy-on-write Set it can die
// with a fatal concurrent map access, which would hide later tests' results.
func TestConcurrentGetSet(t *testing.T) {
	o, _ := baseOptions()
	o.ConfigFileEngine = &stubConfig{}
	s := energize(t, newPattern(), o)

	const workers, iterations = 4, 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := s.Set(keyFirst, fmt.Sprintf("w%d-%d", w, i)); err != nil {
					t.Errorf("Set: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if _, err := s.Get(keyFirst); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations/20; i++ {
				if err := s.Materialize(); err != nil {
					t.Errorf("Materialize: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if err := s.Set(keyFirst, "final"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	wantValue(t, s, keyFirst, "final")
}

// #endregion Regression tests
