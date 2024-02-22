package transporter_test

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/ale/v2/pconsole"
	"github.com/seanmmitchell/transporter"
	"github.com/seanmmitchell/transporter/jsto"

	"os/exec"
)

var testStateCounter int = 0

// Set Static Test Data
type staticTest struct {
	patternKey string
	cliFlag    string
	cliValue   string
}

var sampleCLIArgs1 = []staticTest{
	{"user-firstName", "f", "sean"},
	{"user-lastName", "ln", "test"},
	{"user-age", "age", "21"},
}

var sampleCLIArgs2 = []staticTest{
	{"user-lastName", "ln", "mitchell"},
}

var pCTX *pconsole.PConsoleCTX
var estFP = "transporter_test-" + strconv.Itoa(0) + ".config.json"

func TestMain(t *testing.T) {
	le := ale.CreateLogEngine("Transporter Testing")
	pCTX, _ = pconsole.New(40, 20)
	le.AddLogPipeline(ale.Debug, pCTX.Log)

	// Test Cases
	// No predefined pattern, setting and materializing.
	// Predefined pattern, materializing immediately. - Expected behavior is to create a empty file unless another file is present.

	// Reset CLI Arguments
	os.Args = []string{}
	for _, input := range sampleCLIArgs1 {
		os.Args = append(os.Args, fmt.Sprintf("--%s", input.cliFlag))
		os.Args = append(os.Args, input.cliValue)
	}

	// Energize Transporter State and ensure we Materialize Successfully
	sampleState := createTestState(0, le, t)

	for _, sample := range sampleCLIArgs1 {
		le.Log(ale.Verbose, "Checking sample data for \""+sample.patternKey+"\"")
		sampleval, err1 := sampleState.Get(sample.patternKey)

		if err1 != nil {
			le.Log(ale.Error, "Failed to get transporter key for \""+sample.patternKey+"\"")
			t.FailNow()
		}

		if sampleval != sample.cliValue {
			le.Log(ale.Error, "Transporter key \""+sample.patternKey+"\" value did not match the CLI passed value.\nCLI: "+sample.cliValue+"\nTransporter: "+sampleval)
			t.FailNow()
		}
	}

	err := sampleState.Materialize()

	if err != nil {
		le.Log(ale.Error, "Failed to energize pattern...")
		t.FailNow()
	} else {
		le.Log(ale.Info, "Pattern energized!")
	}

	// Close State
	sampleState = nil

	// Modify Age W/O Transporter
	le.Log(ale.Verbose, "Setting age outside of Transporter to 25.")
	// The bash command to run
	cmdStr := `jq '.["user-age"].Value = "25"' transporter_test-0.config.json > transporter_test-0.config.json.tmp && mv transporter_test-0.config.json.tmp transporter_test-0.config.json`
	// Executing the command through bash -c
	cmd := exec.Command("bash", "-c", cmdStr)
	// Run the command and check for errors
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error executing command: %s\n", err)
		t.FailNow()
	}

	// Remove CLI Arguments
	os.Args = []string{}

	// Open Previous State
	sampleState = createTestState(0, le, t)

	time.Sleep(2 * time.Second)

	// Verify Age Changes
	le.Log(ale.Verbose, "Verifying age outside of Transporter... We expect it to now be 25 once we energize.")
	sampleval, err1 := sampleState.Get("user-age")
	if err1 != nil {
		le.Log(ale.Error, "Failed to get transporter key for \"user-age\"")
		t.FailNow()
	}
	if sampleval != "25" {
		le.Log(ale.Error, "Transporter key \"user-age\" value did not match the out of band passed value. | OOB: 25 | Transporter: "+sampleval)
		t.FailNow()
	} else {
		le.Log(ale.Info, "Transporter key \"user-age\" value DID match the out of band passed value. | OOB: 25 | Transporter: "+sampleval)
	}

	// Set New Last Name
	newFirstname := "sue"
	err6 := sampleState.Set("user-firstName", newFirstname)
	if err6 != nil {
		le.Log(ale.Error, "Transporter key \"user-firstName\" value could not be set.")
		t.FailNow()
	}

	// Save State
	err7 := sampleState.Materialize()
	if err7 != nil {
		le.Log(ale.Error, "Transporter state could not save.")
		t.FailNow()
	}

	// Close State
	sampleState = nil
	// Open Previous State
	sampleState = createTestState(0, le, t)

	// Verify Changed State
	savedNewFirstname, err8 := sampleState.Get("user-firstName")
	if err8 != nil {
		le.Log(ale.Error, "Transporter key \"user-firstName\" value could not be fetched.")
		t.FailNow()
	}
	if newFirstname != savedNewFirstname {
		le.Log(ale.Error, "Transporter failed to modify the key \"user-firstName\", materialize and recall it later.")
		t.FailNow()
	} else {
		le.Log(ale.Info, "Transporter successfully modified a key, materialized and recalled it on a reinitalization.")
	}
}

func createTestState(testID int, le *ale.LogEngine, t *testing.T) *transporter.State {
	// Test ID -1 produces a new state.
	if testID == -1 {
		testStateCounter += 1
		testID = testStateCounter
	}

	le.Log(ale.Debug, "Energizing Test Pattern...")
	tle := le.CreateSubEngine("Test " + strconv.Itoa(testID))
	tle.AddLogPipeline(ale.Debug, pCTX.Log)
	pattern, err := transporter.Energize(
		transporter.Pattern{
			map[string]transporter.PatternSequence{
				sampleCLIArgs1[0].patternKey: {
					Name:        "User's First Name",
					Description: "A variable for holding the User's First Name.",
					CLIFlags:    []string{"f", "fn"},
				},
				sampleCLIArgs1[1].patternKey: {
					Name:               "User's Last Name",
					Description:        "A variable for holding the User's Last Name.",
					CLIFlags:           []string{"l", "ln"},
					DisablePersistence: true,
				},
				sampleCLIArgs1[2].patternKey: {
					Name:        "User's Age",
					Description: "A variable for holding the user's age.",
					CLIFlags:    []string{"a", "age"},
				},
			},
		}, transporter.TransporterOptions{
			EnviormentPrefix:         "T_",
			DumpEnvironmentVariables: false,
			DumpCLIArguments:         false,
			LogEngine:                tle,
			LogEnginePConsoleCTX:     pCTX,
			ConfigFileEngine:         &jsto.JSONConfig{FilePath: estFP, FileLock: &sync.Mutex{}},
		},
	)

	if err != nil {
		le.Log(ale.Critical, "Failed to energize. ERR: "+err.Error())
		t.FailNow()
	}

	return pattern
}
