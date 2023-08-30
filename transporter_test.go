package transporter_test

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/seanmmitchell/ale"
	"github.com/seanmmitchell/ale/pconsole"
	"github.com/seanmmitchell/transporter"
	"github.com/seanmmitchell/transporter/jsto"
)

func TestMain(t *testing.T) {
	le := ale.CreateLogEngine("Test")
	le.AddLogPipeline(ale.Debug, pconsole.Log)

	// Reset Arguments
	os.Args = []string{}
	os.Args = append(os.Args, "--name")
	os.Args = append(os.Args, "sean")
	os.Args = append(os.Args, "--favorite-color")
	os.Args = append(os.Args, "blue")
	os.Args = append(os.Args, "--age")
	os.Args = append(os.Args, "22")

	le.Log(ale.Debug, "Energizing with Transporter...")
	tle := le.CreateSubEngine("Transporter")
	tle.AddLogPipeline(ale.Debug, pconsole.Log)
	pattern, err := transporter.Energize(
		transporter.Pattern{
			map[string]transporter.PatternSequence{
				"user-id": {
					Name:        "User's ID",
					Description: "Description",
					CLIFlags:    []string{"i", "id"},
				},
				"user-age": {
					Name:        "User's Age",
					Description: "A variable for holding the user's age.",
					CLIFlags:    []string{"a", "age"},
				},
				"user-name": {
					Name:        "User's Name",
					Description: "A variable for holding the user's name.",
					CLIFlags:    []string{"n", "name"},
				},
			},
		}, transporter.TransporterOptions{
			EnviormentPrefix:         "T_",
			DumpEnvironmentVariables: false,
			DumpCLIArguments:         false,
			LogEngine:                tle,
			ConfigFileEngine:         &jsto.JSONConfig{FilePath: "transporter_test.config.json", FileLock: &sync.Mutex{}},
		},
	)

	if err != nil {
		le.Log(ale.Critical, "Failed to energize. ERR: "+err.Error())
		os.Exit(1)
	}

	fmt.Println(pattern.Active.Load())

	var val string
	val, _ = pattern.Get("user-age")
	fmt.Println(val)

	pattern.Set("user-age", "2500")

	val, _ = pattern.Get("user-age")
	fmt.Println(val)

	err = pattern.Materialize()

	if err != nil {
		le.Log(ale.Error, "Failed to energize pattern...")
		t.FailNow()
	} else {
		le.Log(ale.Info, "Pattern energized!")
	}
}
