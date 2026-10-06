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

var (
	ErrKeyNotFound         = errors.New("transporter: key does not exist")
	ErrNoConfigFileEngine  = errors.New("transporter: no config file engine configured")
	ErrDuplicateIdentifier = errors.New("transporter: duplicate key/flag/env identifier")
	ErrInvalidIdentifier   = errors.New("transporter: invalid flag/env identifier")
	ErrRequiredMissing     = errors.New("transporter: required value missing")
)

type PatternSequence struct {
	Name               string   `json:"Name"`
	Description        string   `json:"Description"`
	Example            string   `json:"-"`
	Required           bool     `json:"Required"`
	DisablePersistence bool     `json:"DisablePersistence"`
	ENVVars            []string `json:"-"`
	CLIFlags           []string `json:"-"`
	Value              string   `json:"Value"`
}

type Pattern struct {
	Sequences map[string]PatternSequence
}

type State struct {
	// active is replaced, never modified, once published.
	active atomic.Pointer[Pattern]
	// saveMu orders Materialize calls so an older snapshot never overwrites a newer one.
	saveMu sync.Mutex

	logEngine          *ale.LogEngine
	configLogEngine    *ale.LogEngine
	transporterOptions TransporterOptions
}

type TransporterOptions struct {
	//// Logging
	// LogEngine defaults to an engine that emits Warning and above to the console.
	LogEngine            *ale.LogEngine
	LogEnginePConsoleCTX *pconsole.PConsoleCTX

	//// Loading
	// Environment: only variables carrying this prefix are read. Defaults to DefaultEnvironmentPrefix.
	EnvironmentPrefix string
	// Environ defaults to os.Environ().
	Environ []string
	// CLI: Args defaults to os.Args[1:].
	Args []string
	// ConfigFile (jsto): ConfigFileLogEngine defaults to the transporter log engine.
	ConfigFileLogEngine *ale.LogEngine
	ConfigFileEngine    ConfigFileInterface

	//// Dev / Debug
	DumpEnvironmentVariables bool
	DumpCLIArguments         bool
}

// ConfigFileInterface persists patterns. Load must return an error wrapping
// fs.ErrNotExist when the backing file does not exist.
type ConfigFileInterface interface {
	Load(le *ale.LogEngine) (map[string]interface{}, error)
	Save(le *ale.LogEngine, pattern *Pattern) error
}

const DefaultEnvironmentPrefix = "T_"

const CONF_DisablePersistence_Phrase = "no persistence"

// Energize fills a copy of pattern from the config file, the environment and
// the CLI, in that order, with later sources overriding earlier ones.
// Values are never logged.
func Energize(pattern Pattern, tOpts TransporterOptions) (*State, error) {
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
// DisablePersistence values are replaced by CONF_DisablePersistence_Phrase in
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
			seq.Value = CONF_DisablePersistence_Phrase
			snapshot.Sequences[key] = seq
		}
	}

	if err := state.transporterOptions.ConfigFileEngine.Save(state.configLogEngine, &snapshot); err != nil {
		return fmt.Errorf("transporter: saving config file: %w", err)
	}
	return nil
}

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

// buildIndex maps every sequence key and every identifier returned by ids to
// its sequence key. An identifier claimed by two sequences is an error, as is
// one that could never match: empty, containing "=" or, when badPrefix is set,
// starting with it.
func buildIndex(pattern Pattern, kind string, badPrefix string, ids func(PatternSequence) []string) (map[string]string, error) {
	index := make(map[string]string)
	claim := func(id string, key string) error {
		if id == "" {
			return nil
		}
		if owner, ok := index[id]; ok && owner != key {
			return fmt.Errorf("%w: %s %q is used by both %q and %q", ErrDuplicateIdentifier, kind, id, owner, key)
		}
		index[id] = key
		return nil
	}

	keys := sortedKeys(pattern.Sequences)
	for _, key := range keys {
		if err := claim(key, key); err != nil {
			return nil, err
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
