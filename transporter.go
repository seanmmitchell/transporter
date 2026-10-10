// Package transporter collects a program's settings from a JSON config file,
// prefixed environment variables and command-line flags into one State.
//
// Describe each setting as a PatternSequence, call Energize to load the
// sources (config file < environment < CLI, later sources win), then read
// and change values with State.Get and State.Set and save them with
// State.Materialize. Values are never logged.
package transporter

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/ale/v2/pconsole"
)

// Errors returned by this package, wrapped with context; test with errors.Is.
var (
	// ErrKeyNotFound is returned by Get and Set for a key not in the pattern.
	ErrKeyNotFound = errors.New("transporter: key does not exist")
	// ErrNoConfigFileEngine is returned by Materialize when Options.ConfigFileEngine is nil.
	ErrNoConfigFileEngine = errors.New("transporter: no config file engine configured")
	// ErrDuplicateIdentifier is returned by Energize when a key, CLI flag or
	// environment name belongs to more than one sequence within one source.
	ErrDuplicateIdentifier = errors.New("transporter: duplicate key/flag/env identifier")
	// ErrInvalidIdentifier is returned by Energize for a CLI flag or
	// environment name that could never match (empty, containing "=", or a
	// flag starting with "-").
	ErrInvalidIdentifier = errors.New("transporter: invalid flag/env identifier")
	// ErrRequiredMissing is returned by Energize when a Required sequence has
	// no value after all sources are loaded.
	ErrRequiredMissing = errors.New("transporter: required value missing")
)

// PatternSequence describes one setting: where its value may come from and
// how it is persisted. Its key in Pattern.Sequences names it.
type PatternSequence struct {
	// Name and Description document the setting; they are saved with it.
	Name        string `json:"Name"`
	Description string `json:"Description"`
	// Example documents a sample value; it is not saved.
	Example string `json:"-"`
	// Required makes Energize fail with ErrRequiredMissing when Value is
	// still empty after loading.
	Required bool `json:"Required"`
	// DisablePersistence keeps the value out of the config file: Materialize
	// saves DisablePersistencePhrase instead, and loading skips the entry.
	DisablePersistence bool `json:"DisablePersistence"`
	// ENVVars are environment variable names, without the prefix, that set
	// this sequence. A non-empty sequence key also works.
	ENVVars []string `json:"-"`
	// CLIFlags are flag names, without leading dashes, that set this
	// sequence. A non-empty sequence key also works.
	CLIFlags []string `json:"-"`
	// Value is the default. Energize works on a copy, so read the loaded
	// value with State.Get.
	Value string `json:"Value"`
}

// Pattern is the full set of settings, keyed by sequence key.
type Pattern struct {
	Sequences map[string]PatternSequence
}

// State holds the loaded values. Obtain one from Energize; the zero value is
// not usable. A State is safe for concurrent use.
type State struct {
	// active is replaced, never modified, once published.
	active atomic.Pointer[Pattern]
	// saveMu orders Materialize calls so an older snapshot never overwrites a newer one.
	saveMu sync.Mutex

	logEngine          *ale.LogEngine
	configLogEngine    *ale.LogEngine
	transporterOptions Options
}

// Options configures Energize. The zero value is usable.
type Options struct {
	// LogEngine receives Transporter's logs. When nil, a console engine that
	// emits Warning and above is created.
	LogEngine *ale.LogEngine
	// LogEnginePConsoleCTX is used only to build the default LogEngine.
	LogEnginePConsoleCTX *pconsole.PConsoleCTX

	// EnvironmentPrefix limits which environment variables are read.
	// Empty means DefaultEnvironmentPrefix.
	EnvironmentPrefix string
	// Environ lists "NAME=value" entries to read. Nil means os.Environ();
	// use an empty slice for none.
	Environ []string
	// Args lists the CLI arguments to parse. Nil means os.Args[1:]; use an
	// empty slice for none.
	Args []string

	// ConfigFileLogEngine receives the config file engine's logs. Nil means LogEngine.
	ConfigFileLogEngine *ale.LogEngine
	// ConfigFileEngine loads and saves the config file, e.g. jsto.New(path).
	// Nil disables the config file.
	ConfigFileEngine ConfigFileInterface

	// DumpEnvironmentVariables and DumpCLIArguments log the names of the
	// prefixed variables and the parsed CLI flags at Debug level, with every
	// value redacted. They need a LogEngine with a Debug pipeline.
	DumpEnvironmentVariables bool
	DumpCLIArguments         bool
}

// ConfigFileInterface persists patterns. Load must return an error wrapping
// fs.ErrNotExist when the backing file does not exist. Save must not call
// State.Materialize, which would deadlock.
type ConfigFileInterface interface {
	Load(le *ale.LogEngine) (map[string]interface{}, error)
	Save(le *ale.LogEngine, pattern *Pattern) error
}

// DefaultEnvironmentPrefix is used when Options.EnvironmentPrefix is empty.
const DefaultEnvironmentPrefix = "T_"

// DisablePersistencePhrase is saved in place of a DisablePersistence value.
const DisablePersistencePhrase = "no persistence"

// Energize fills a copy of pattern from the config file, the environment and
// the CLI, in that order, with later sources overriding earlier ones.
// Values are never logged.
func Energize(pattern Pattern, tOpts Options) (*State, error) {
	// Work on a copy so the caller's pattern is never mutated.
	pattern = clonePattern(pattern)

	if tOpts.EnvironmentPrefix == "" {
		tOpts.EnvironmentPrefix = DefaultEnvironmentPrefix
	}
	state := &State{transporterOptions: tOpts}

	// Load Transporter ALE Log Engine
	le := tOpts.LogEngine
	if le == nil {
		pCTX := tOpts.LogEnginePConsoleCTX
		if pCTX == nil {
			var err error
			pCTX, err = pconsole.New(30, 20)
			if err != nil {
				return nil, err
			}
		}
		le = ale.CreateLogEngine("Transporter")
		le.AddLogPipeline(ale.Warning, pCTX.Log)
	}
	state.logEngine = le

	// Load ConfigFile ALE Log Engine
	configLE := le
	if tOpts.ConfigFileLogEngine != nil {
		configLE = tOpts.ConfigFileLogEngine
	}
	state.configLogEngine = configLE

	le.Log(ale.Info, "Energizing...")

	// Index identifiers per source; each must belong to a single sequence.
	// Flag names are given without dashes; neither kind of name may contain "=".
	cliIndex, err := buildIndex(pattern, "CLI flag", "-", func(seq PatternSequence) []string { return seq.CLIFlags })
	if err != nil {
		return nil, err
	}
	envIndex, err := buildIndex(pattern, "environment variable", "", func(seq PatternSequence) []string { return seq.ENVVars })
	if err != nil {
		return nil, err
	}

	// Load Config (matches sequence keys only)
	if tOpts.ConfigFileEngine != nil {
		configLE.Log(ale.Verbose, "Loading config file...")
		confData, err := tOpts.ConfigFileEngine.Load(configLE)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			configLE.Log(ale.Info, "Config file does not exist, skipping.")
		case err != nil:
			// Fail rather than let a later Materialize overwrite the file.
			return nil, fmt.Errorf("transporter: loading config file: %w", err)
		default:
			loadConfig(configLE, &pattern, confData)
			configLE.Log(ale.Verbose, "Config file loaded.")
		}
	}

	// Load Environment Variables (matches envIndex only)
	environ := tOpts.Environ
	if environ == nil {
		environ = os.Environ()
	}
	if tOpts.DumpEnvironmentVariables {
		dumpEnvironmentVariables(le, environ, tOpts.EnvironmentPrefix)
	}
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, tOpts.EnvironmentPrefix) {
			continue
		}

		key, ok := envIndex[strings.TrimPrefix(name, tOpts.EnvironmentPrefix)]
		if !ok {
			le.Log(ale.Warning, fmt.Sprintf("Environment variable %q does not match any pattern, skipping.", name))
			continue
		}
		setValue(&pattern, key, value)
		le.Log(ale.Verbose, fmt.Sprintf("Environment variable %q assigned to key %q.", name, key))
	}

	// Load CLI Args (matches cliIndex only)
	args := tOpts.Args
	if args == nil && len(os.Args) > 1 {
		args = os.Args[1:]
	}
	// tokens records what the parser identified, so the debug dump can show
	// known flags and redact everything else (values, positionals, unknowns).
	tokens := make([]cliToken, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			tokens[i] = cliToken{flag: arg}
			break
		}

		// Accept "--name", "-name" and "--name=value"; a lone "-" is positional.
		var name string
		switch {
		case strings.HasPrefix(arg, "--"):
			name = arg[2:]
		case strings.HasPrefix(arg, "-") && arg != "-":
			name = arg[1:]
		default:
			le.Log(ale.Verbose, fmt.Sprintf("Skipping positional CLI argument at index %d.", i))
			continue
		}
		name, value, hasValue := strings.Cut(name, "=")

		key, ok := cliIndex[name]
		if !ok {
			// Unknown flags do not consume the next argument, so this token may
			// really be a value (e.g. "-s3cret"); log its index, never its text.
			le.Log(ale.Warning, fmt.Sprintf("Unknown CLI flag at index %d, skipping.", i))
			continue
		}
		flag, _, _ := strings.Cut(arg, "=")
		tokens[i] = cliToken{flag: flag, inline: hasValue}
		if !hasValue {
			if i+1 >= len(args) || args[i+1] == "--" {
				le.Log(ale.Warning, fmt.Sprintf("CLI flag %q is missing a value, skipping.", name))
				continue
			}
			// The next argument is the value even if it starts with "-".
			i++
			value = args[i]
		}
		setValue(&pattern, key, value)
		le.Log(ale.Verbose, fmt.Sprintf("CLI flag %q assigned to key %q.", name, key))
	}
	if tOpts.DumpCLIArguments {
		dumpCLIVariables(le, tokens)
	}

	// Check Required
	var missing []string
	for _, key := range sortedKeys(pattern.Sequences) {
		if seq := pattern.Sequences[key]; seq.Required && seq.Value == "" {
			missing = append(missing, strconv.Quote(key))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrRequiredMissing, strings.Join(missing, ", "))
	}

	le.Log(ale.Info, "Energized!")

	state.active.Store(&pattern)
	return state, nil
}

// Materialize saves the active pattern through the config file engine.
// DisablePersistence values are replaced by DisablePersistencePhrase in
// the saved copy only.
func (state *State) Materialize() error {
	if state.transporterOptions.ConfigFileEngine == nil {
		return ErrNoConfigFileEngine
	}

	state.saveMu.Lock()
	defer state.saveMu.Unlock()

	snapshot := clonePattern(*state.active.Load())
	for key, seq := range snapshot.Sequences {
		if seq.DisablePersistence {
			seq.Value = DisablePersistencePhrase
			snapshot.Sequences[key] = seq
		}
	}

	if err := state.transporterOptions.ConfigFileEngine.Save(state.configLogEngine, &snapshot); err != nil {
		return fmt.Errorf("transporter: saving config file: %w", err)
	}
	return nil
}

// Get returns the current value for key, or an error wrapping ErrKeyNotFound.
func (state *State) Get(key string) (string, error) {
	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Getting value for key %q...", key))

	seq, ok := state.active.Load().Sequences[key]
	if !ok {
		state.logEngine.Log(ale.Warning, fmt.Sprintf("Key %q does not exist.", key))
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, key)
	}

	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Retrieved value for key %q.", key))
	return seq.Value, nil
}

// Set changes the in-memory value for key, or returns an error wrapping
// ErrKeyNotFound. Call Materialize to save it.
func (state *State) Set(key string, value string) error {
	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Setting a new value for key %q...", key))

	// Copy-on-write: published patterns are shared with readers, so publish a
	// modified copy and retry if another Set won the race.
	for {
		old := state.active.Load()
		if _, ok := old.Sequences[key]; !ok {
			state.logEngine.Log(ale.Warning, fmt.Sprintf("Key %q does not exist.", key))
			return fmt.Errorf("%w: %q", ErrKeyNotFound, key)
		}

		next := clonePattern(*old)
		setValue(&next, key, value)
		if state.active.CompareAndSwap(old, &next) {
			break
		}
	}

	state.logEngine.Log(ale.Verbose, fmt.Sprintf("New value set for key %q.", key))
	return nil
}

// loadConfig assigns config file values to sequences with matching keys.
func loadConfig(le *ale.LogEngine, pattern *Pattern, confData map[string]interface{}) {
	for _, confKey := range sortedKeys(confData) {
		seq, ok := pattern.Sequences[confKey]
		if !ok {
			le.Log(ale.Warning, fmt.Sprintf("Config file key %q does not match any pattern, skipping.", confKey))
			continue
		}
		confVal, ok := confData[confKey].(map[string]interface{})
		if !ok {
			le.Log(ale.Warning, fmt.Sprintf("Config file entry %q is not an object, skipping.", confKey))
			continue
		}

		// Skip non-persistent sequences; a missing or non-bool file flag counts as false.
		fileDisabled, _ := confVal["DisablePersistence"].(bool)
		if seq.DisablePersistence || fileDisabled {
			continue
		}

		value, ok := confVal["Value"].(string)
		if !ok {
			le.Log(ale.Warning, fmt.Sprintf("Config file entry %q has no string value, skipping.", confKey))
			continue
		}
		seq.Value = value
		pattern.Sequences[confKey] = seq
	}
}

// buildIndex maps every sequence key (except an empty one) and every
// identifier returned by ids to its sequence key. An identifier claimed by
// two sequences is an error, as is
// one that could never match: empty, containing "=" or, when badPrefix is set,
// starting with it.
func buildIndex(pattern Pattern, kind string, badPrefix string, ids func(PatternSequence) []string) (map[string]string, error) {
	index := make(map[string]string)
	claim := func(id string, key string) error {
		if owner, ok := index[id]; ok && owner != key {
			return fmt.Errorf("%w: %s %q is used by both %q and %q", ErrDuplicateIdentifier, kind, id, owner, key)
		}
		index[id] = key
		return nil
	}

	// Keys are unique map keys, so they never collide with each other.
	keys := sortedKeys(pattern.Sequences)
	for _, key := range keys {
		if key != "" {
			index[key] = key
		}
	}
	for _, key := range keys {
		for _, id := range ids(pattern.Sequences[key]) {
			if id == "" || strings.Contains(id, "=") || (badPrefix != "" && strings.HasPrefix(id, badPrefix)) {
				return nil, fmt.Errorf("%w: %s %q of %q can never match", ErrInvalidIdentifier, kind, id, key)
			}
			if err := claim(id, key); err != nil {
				return nil, err
			}
		}
	}
	return index, nil
}

// setValue assigns value to the sequence at key, which must exist.
func setValue(pattern *Pattern, key string, value string) {
	seq := pattern.Sequences[key]
	seq.Value = value
	pattern.Sequences[key] = seq
}

// clonePattern returns a deep copy of p; a nil Sequences map becomes empty.
func clonePattern(p Pattern) Pattern {
	c := Pattern{Sequences: make(map[string]PatternSequence, len(p.Sequences))}
	for key, seq := range p.Sequences {
		seq.CLIFlags = slices.Clone(seq.CLIFlags)
		seq.ENVVars = slices.Clone(seq.ENVVars)
		c.Sequences[key] = seq
	}
	return c
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
