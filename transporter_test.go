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
	os.Args = append(os.Args, "--f")
	os.Args = append(os.Args, "sean")
	os.Args = append(os.Args, "--ln")
	os.Args = append(os.Args, "test")
	os.Args = append(os.Args, "--age")
	os.Args = append(os.Args, "21")
	os.Args = append(os.Args, "--role")
	os.Args = append(os.Args, "admin")

	le.Log(ale.Debug, "Energizing with Transporter...")
	tle := le.CreateSubEngine("Transporter")
	tle.AddLogPipeline(ale.Debug, pconsole.Log)
	pattern, err := transporter.Energize(
		transporter.Pattern{
			map[string]transporter.PatternSequence{
				"user-firstName": {
					Name:        "User's First Name",
					Description: "A variable for holding the User's First Name.",
					CLIFlags:    []string{"f", "fn"},
				},
				"user-lastName": {
					Name:               "User's Last Name",
					Description:        "A variable for holding the User's Last Name.",
					CLIFlags:           []string{"l", "ln"},
					DisablePersistence: true,
				},
				"user-age": {
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
			ConfigFileEngine:         &jsto.JSONConfig{FilePath: "transporter_test.config.json", FileLock: &sync.Mutex{}},
		},
	)

	if err != nil {
		le.Log(ale.Critical, "Failed to energize. ERR: "+err.Error())
		os.Exit(1)
	}

	fmt.Println(pattern.Active.Load())

	var val string
	val, _ = pattern.Get("user-lastName")
	fmt.Println("User Last Name: ", val)

	val, _ = pattern.Get("user-age")
	fmt.Println("User Age: ", val)

	pattern.Set("user-age", "40")

	val, _ = pattern.Get("user-age")
	fmt.Println("User Age: ", val)

	err = pattern.Materialize()

	if err != nil {
		le.Log(ale.Error, "Failed to energize pattern...")
		t.FailNow()
	} else {
		le.Log(ale.Info, "Pattern energized!")
	}
}
