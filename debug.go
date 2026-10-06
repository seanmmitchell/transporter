package transporter

import (
	"fmt"

	"github.com/seanmmitchell/ale/v2"
)

func dumpEnvironmentVariables(le *ale.LogEngine, environ []string, prefix string) {
	envDump := ""
	for _, envVar := range environ {
		envDump += fmt.Sprintf("\n\t\t==> %s", envVar)
	}
	le.Log(ale.Debug, "Dumping Enviorment Variables: "+envDump)
}

func dumpCLIVariables(le *ale.LogEngine, args []string) {
	cliDump := ""
	for _, cliVar := range args {
		cliDump += fmt.Sprintf("\n\t\t==> %s", cliVar)
	}
	le.Log(ale.Debug, "Dumping CLI Variables: "+cliDump)
}
