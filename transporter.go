package transporter

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/seanmmitchell/ale"
	"github.com/seanmmitchell/ale/pconsole"
)

type PatternSequence struct {
	Name               string   `json:"Name"`
	Description        string   `json:"Description"`
	Example            string   `json:"-"`
	Required           bool     `json:"Required"`
	DisablePersistence bool     `json:"DisablePersistence"`
	ENVCaseSensitive   bool     `json:"-"`
	ENVVars            []string `json:"-"`
	CaseSensitiveFlags bool     `json:"-"`
	CLIFlags           []string `json:"-"`
	Value              string   `json:"Value"`
}

type Pattern struct {
	Sequences map[string]PatternSequence
}

type State struct {
	Active *atomic.Pointer[Pattern]
	Stored *atomic.Pointer[Pattern]

	logEngine          *ale.LogEngine
	transporterOptions TransporterOptions
}

type TransporterOptions struct {
	//// Logging
	LogEngine *ale.LogEngine

	//// Loading
	// Envioronment
	EnviormentPrefix string
	// CLI
	// Config
	ConfigFilePath   string
	ConfigFileEngine ConfigFileInterface

	//// Dev / Debug
	DumpEnvironmentVariables any
	DumpCLIArguments         any
}

type ConfigFileInterface interface {
	Load(le *ale.LogEngine) (map[string]interface{}, error)
	Save(le *ale.LogEngine, pattern *Pattern) error
}

var defaultTransporterOpts = TransporterOptions{
	ConfigFileEngine:         nil, // Initialize with nil or assign a pointer to an implementation
	DumpEnvironmentVariables: false,
	DumpCLIArguments:         false,
}

const CONF_DisablePersistence_Phrase = "no persistence"

func Energize(pattern Pattern, tOpts TransporterOptions) (*State, error) {
	// Permitted Pattern Names: a-zA-Z0-9.-_
	// Pattern Map
	state := &State{transporterOptions: tOpts}

	state.Active = &atomic.Pointer[Pattern]{}
	state.Stored = &atomic.Pointer[Pattern]{}

	// Load ALE Log Engine
	var le *ale.LogEngine
	if tOpts.LogEngine == nil {
		le = ale.CreateLogEngine("Transporter")
		le.AddLogPipeline(ale.Debug, pconsole.Log)
	} else {
		le = tOpts.LogEngine
	}
	state.logEngine = le

	le.Log(ale.Info, "Energizing...")

	// Load Config
	if tOpts.ConfigFileEngine != nil {
		jstonLE := le.CreateSubEngine("JSTO")
		jstonLE.AddLogPipeline(ale.Debug, pconsole.Log)
		jstonLE.Log(ale.Verbose, "Loading JSON...")
		jsonData, err := tOpts.ConfigFileEngine.Load(jstonLE)
		if err == nil {
			jstonLE.Log(ale.Verbose, "JSON Loaded.")

			for confKey, confData := range jsonData {
				confVal, ok := confData.(map[string]interface{})
				if ok {
					// Check if pattern has persistence.
					disabledPersitence, ok := confVal["DisabledPersitence"].(bool)
					if !ok || disabledPersitence {
						continue
					}

					// Get the Pattern
					value, ok := confVal["Value"].(string)
					if ok {
						associatePattern(jstonLE, &pattern, confKey, value)
					} else {
						jstonLE.Log(ale.Warning, fmt.Sprintf("JSON value not found. Key: %s", confKey))
					}
				} else {
					jstonLE.Log(ale.Warning, fmt.Sprintf("JSON value unable to be parsed. Key: %s", confKey))
				}
			}
		} else {
			jstonLE.Log(ale.Warning, "JSON Failed to Load.")
		}
	}

	// Load Enviorment Args
	if tOpts.DumpEnvironmentVariables == true || (tOpts.DumpEnvironmentVariables == nil && defaultTransporterOpts.DumpEnvironmentVariables == true) {
		dumpEnvironmentVariables(le)
	}
	for _, ENVArg := range os.Environ() {
		if !strings.HasPrefix(ENVArg, tOpts.EnviormentPrefix) {
			continue
		}

		parts := strings.Split(ENVArg, "=")
		if len(parts) == 2 {
			argName := (parts[0])[len(tOpts.EnviormentPrefix):]
			argValue := parts[1]
			le.Log(ale.Debug, fmt.Sprintf("New Explicit ENV Arg Found >\n\t> Flag: %s\n\t> Value: %s", argName, argValue))

			associatePattern(le, &pattern, argName, argValue)
		} else {
			le.Log(ale.Error, "Invalid ENV Argument, skipping.")
		}
	}

	// Load CLI Arg
	if tOpts.DumpCLIArguments == true || (tOpts.DumpCLIArguments == nil && defaultTransporterOpts.DumpCLIArguments == true) {
		dumpCLIVariables(le)
	}

	for indexOfCLIArgs := 0; indexOfCLIArgs < len(os.Args); indexOfCLIArgs++ {
		arg := os.Args[indexOfCLIArgs]

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
		if len(os.Args) > indexOfCLIArgs+1 {
			argValue = os.Args[indexOfCLIArgs+1]
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

	(*state.Active).Store(&pattern)
	return state, nil
}

func (state *State) Materialize() error {
	// Filter sequences to remove anything without Persistence.
	var oldPattern Pattern = (*state.Active.Load())

	for key, pattern := range oldPattern.Sequences {
		if pattern.DisablePersistence {
			pattern.Value = CONF_DisablePersistence_Phrase
			oldPattern.Sequences[key] = pattern
		}
	}

	state.transporterOptions.ConfigFileEngine.Save(state.logEngine, state.Active.Swap(&oldPattern))
	return nil
}

func (state *State) Switch() error {
	return nil
}

func (state *State) Get(key string) (string, error) {
	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Getting value for key \"%s\"...", key))
	pat := state.Active.Load()

	value, ok := pat.Sequences[key]
	if !ok {
		state.logEngine.Log(ale.Warning, fmt.Sprintf("Key \"%s\" does not exist.", key))
		return "", fmt.Errorf("key does not exist")
	}

	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Retrieved value for key \"%s\".", key))
	return value.Value, nil
}

func (state *State) Set(key string, value string) error {
	state.logEngine.Log(ale.Verbose, fmt.Sprintf("Setting a new value for key \"%s\"...", key))
	pat := state.Active.Load()

	existingValue, ok := pat.Sequences[key]
	if !ok {
		state.logEngine.Log(ale.Warning, fmt.Sprintf("Key \"%s\" does not exist.", key))
		return fmt.Errorf("key does not exist")
	}

	existingValue.Value = value

	pat.Sequences[key] = existingValue

	state.logEngine.Log(ale.Verbose, fmt.Sprintf("New value set for key \"%s\".", key))
	return nil
}

func associatePattern(le *ale.LogEngine, pattern *Pattern, confIdentifier string, confValue string) bool {
	le.Log(ale.Debug, "Searching for Pattern...")
	matchFound := false
	for indexOfSequence, sequence := range pattern.Sequences {
		//fmt.Printf("=================\n\tSTART\n\t\tKey: %s \n\t\tValue: %s\n\t\tName: %s\n", key, seq.Value, seq.Name)
		for indexOfFlag := 0; indexOfFlag < len(sequence.CLIFlags); indexOfFlag++ {

			seqCLIFlag := sequence.CLIFlags[indexOfFlag]
			if confIdentifier == seqCLIFlag || confIdentifier == indexOfSequence {
				le.Log(ale.Verbose, fmt.Sprintf("A pattern was located \"%s\"", confIdentifier))
				sequence.Value = confValue
				matchFound = true
				le.Log(ale.Verbose, fmt.Sprintf("The value was assigned to the pattern \"%s\"", confIdentifier))
				break
			}
		}

		pattern.Sequences[indexOfSequence] = sequence
		//fmt.Printf("\n\tEND\n\t\tKey: %s \n\t\tValue: %s\n\t\tName: %s\n=================\n", key, seq.Value, seq.Name)
	}

	if matchFound {
		le.Log(ale.Debug, "Pattern Found.")
		return true
	} else {
		le.Log(ale.Warning, fmt.Sprintf("No Pattern was Located for \"%s\"", confIdentifier))
		return false
	}
}
