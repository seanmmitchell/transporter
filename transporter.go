package transporter

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/ale/v2/pconsole"
)

var (
	ErrKeyNotFound         = errors.New("transporter: key does not exist")
	ErrNoConfigFileEngine  = errors.New("transporter: no config file engine configured")
	ErrDuplicateIdentifier = errors.New("transporter: duplicate key/flag/env identifier")
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
	active atomic.Pointer[Pattern]

	logEngine          *ale.LogEngine
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

func Energize(pattern Pattern, tOpts TransporterOptions) (*State, error) {
	// Permitted Pattern Names: a-zA-Z0-9.-_
	// Pattern Map
	state := &State{transporterOptions: tOpts}

	// Load Transporter ALE Log Engine
	var pCTX *pconsole.PConsoleCTX
	if tOpts.LogEnginePConsoleCTX == nil {
		pCTX, _ = pconsole.New(30, 20)
	} else {
		pCTX = tOpts.LogEnginePConsoleCTX
	}
	var le *ale.LogEngine
	if tOpts.LogEngine == nil {
		le = ale.CreateLogEngine("Transporter")
		le.AddLogPipeline(ale.Debug, pCTX.Log)
	} else {
		le = tOpts.LogEngine
	}
	state.logEngine = le

	le.Log(ale.Info, "Energizing...")

	// Load Config
	if tOpts.ConfigFileEngine != nil {
		// Load ConfigFile ALE Log Engine
		configFilele := le
		if tOpts.ConfigFileLogEngine != nil {
			configFilele = tOpts.ConfigFileLogEngine
		}

		configFilele.Log(ale.Verbose, "Loading JSON...")
		jsonData, err := tOpts.ConfigFileEngine.Load(configFilele)
		if err == nil {
			configFilele.Log(ale.Verbose, "JSON Loaded.")

			for confKey, confData := range jsonData {
				confVal, ok := confData.(map[string]interface{})
				if ok {
					// Check if pattern has persistence.
					disabledPersitence, ok := confVal["DisablePersistence"].(bool)
					if !ok || disabledPersitence {
						continue
					}

					// Get the Pattern
					value, ok := confVal["Value"].(string)
					if ok {
						associatePattern(configFilele, &pattern, confKey, value)
					} else {
						configFilele.Log(ale.Warning, fmt.Sprintf("JSON value not found. Key: %s", confKey))
					}
				} else {
					configFilele.Log(ale.Warning, fmt.Sprintf("JSON value unable to be parsed. Key: %s", confKey))
				}
			}
		} else {
			configFilele.Log(ale.Warning, "JSON Failed to Load.")
		}
	}

	// Load Enviorment Args
	environ := tOpts.Environ
	if environ == nil {
		environ = os.Environ()
	}
	if tOpts.DumpEnvironmentVariables {
		dumpEnvironmentVariables(le, environ, tOpts.EnvironmentPrefix)
	}
	for _, ENVArg := range environ {
		if !strings.HasPrefix(ENVArg, tOpts.EnvironmentPrefix) {
			continue
		}

		parts := strings.Split(ENVArg, "=")
		if len(parts) == 2 {
			argName := (parts[0])[len(tOpts.EnvironmentPrefix):]
			argValue := parts[1]
			le.Log(ale.Debug, fmt.Sprintf("New Explicit ENV Arg Found >\n\t> Flag: %s\n\t> Value: %s", argName, argValue))

			associatePattern(le, &pattern, argName, argValue)
		} else {
			le.Log(ale.Error, "Invalid ENV Argument, skipping.")
		}
	}

	// Load CLI Arg
	args := tOpts.Args
	if args == nil && len(os.Args) > 1 {
		args = os.Args[1:]
	}
	if tOpts.DumpCLIArguments {
		dumpCLIVariables(le, args)
	}

	for indexOfCLIArgs := 0; indexOfCLIArgs < len(args); indexOfCLIArgs++ {
		arg := args[indexOfCLIArgs]

		var argName string
		var argValue string
		// Try to detect a flag so we can associate the input with a value
		if len(arg) > 1 && arg[:1] == "-" {
			argName = arg[2:]
		} else if len(arg) > 2 && arg[:2] == "--" {
			argName = arg[3:]
		} else {
			// Catch any unexpected input. We should be recieving a flag before a value.
			le.Log(ale.Warning, "Skipping an invalid CLI argument. Argument \""+arg+"\"")
			continue
		}

		// Explicit Definition
		if len(args) > indexOfCLIArgs+1 {
			argValue = args[indexOfCLIArgs+1]
		} else {
			le.Log(ale.Warning, fmt.Sprintf("Failed to seek value for arg \"%s\"", argName))
			continue
		}
		//le.Log(ale.Debug, fmt.Sprintf("New Explicit CLI Arg Found >\n\t> Flag: %s\n\t> Value: %s", argName, argValue))

		// Fetch the Pattern
		matchFound := associatePattern(le, &pattern, argName, argValue)
		if matchFound {
			indexOfCLIArgs = indexOfCLIArgs + 1
		}
	}

	le.Log(ale.Info, "Energized!")

	state.active.Store(&pattern)
	return state, nil
}

func (state *State) Materialize() error {
	// Filter sequences to remove anything without Persistence.
	var oldPattern Pattern = (*state.active.Load())

	for key, pattern := range oldPattern.Sequences {
		if pattern.DisablePersistence {
			pattern.Value = CONF_DisablePersistence_Phrase
			pattern.Value = CONF_DisablePersistence_Phrase
			oldPattern.Sequences[key] = pattern
		}
	}

	state.transporterOptions.ConfigFileEngine.Save(state.logEngine, state.active.Swap(&oldPattern))
	return nil
}

func (state *State) Get(key string) (string, error) {
	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Getting value for key \"%s\"...", key))
	pat := state.active.Load()

	value, ok := pat.Sequences[key]
	if !ok {
		state.logEngine.Log(ale.Warning, fmt.Sprintf("Key \"%s\" does not exist.", key))
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, key)
	}

	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Retrieved value for key \"%s\".", key))
	return value.Value, nil
}

func (state *State) Set(key string, value string) error {
	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Setting a new value for key \"%s\"...", key))
	pat := state.active.Load()

	existingValue, ok := pat.Sequences[key]
	if !ok {
		state.logEngine.Log(ale.Warning, fmt.Sprintf("Key \"%s\" does not exist.", key))
		return fmt.Errorf("%w: %q", ErrKeyNotFound, key)
	}

	existingValue.Value = value

	pat.Sequences[key] = existingValue

	state.logEngine.Log(ale.Verbose, fmt.Sprintf("New value set for key \"%s\".", key))
	return nil
}

func associatePattern(le *ale.LogEngine, pattern *Pattern, confIdentifier string, confValue string) bool {
	le.Log(ale.Debug, "Searching for Pattern...")
	matchFound := false

	// Check Each Sequence
	for indexOfSequence, sequence := range pattern.Sequences {
		// Check Identifier Match (JSON, ENV, CLI CAN HIT)
		if indexOfSequence == confIdentifier {
			le.Log(ale.Verbose, fmt.Sprintf("A config identifier pattern was located \"%s\"", confIdentifier))
			sequence.Value = confValue
			matchFound = true
			le.Log(ale.Verbose, fmt.Sprintf("The config identifier value was assigned to the pattern \"%s\"", confIdentifier))
		}

		if matchFound {
			pattern.Sequences[indexOfSequence] = sequence
			break
		}

		// Check CLI Match (ENV, CLI CAN HIT)
		for indexOfFlag := 0; indexOfFlag < len(sequence.CLIFlags); indexOfFlag++ {
			seqCLIFlag := sequence.CLIFlags[indexOfFlag]
			if confIdentifier == seqCLIFlag || confIdentifier == indexOfSequence {
				le.Log(ale.Verbose, fmt.Sprintf("A cli pattern was located \"%s\"", confIdentifier))
				sequence.Value = confValue
				matchFound = true
				le.Log(ale.Verbose, fmt.Sprintf("The cli value was assigned to the pattern \"%s\"", confIdentifier))
				break
			}
		}

		if matchFound {
			pattern.Sequences[indexOfSequence] = sequence
			break
		}

		// Check ENV Match (ENVCAN HIT)
		for indexOfFlag := 0; indexOfFlag < len(sequence.ENVVars); indexOfFlag++ {
			seqENVFlag := sequence.ENVVars[indexOfFlag]
			if confIdentifier == seqENVFlag || confIdentifier == indexOfSequence {
				le.Log(ale.Verbose, fmt.Sprintf("A env pattern was located \"%s\"", confIdentifier))
				sequence.Value = confValue
				matchFound = true
				le.Log(ale.Verbose, fmt.Sprintf("The env value was assigned to the pattern \"%s\"", confIdentifier))
				break
			}
		}

		if matchFound {
			pattern.Sequences[indexOfSequence] = sequence
			break
		}
	}

	if matchFound {
		le.Log(ale.Debug, "Pattern Found.")
		return true
	} else {
		le.Log(ale.Warning, fmt.Sprintf("No Pattern was Located for \"%s\"", confIdentifier))
		return false
	}
}
