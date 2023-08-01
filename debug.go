package transporter

import (
	"fmt"
	"os"

	"github.com/seanmmitchell/ale"
)

func dumpEnvironmentVariables(le *ale.LogEngine) {
	envDump := ""
	for _, envVar := range os.Environ() {
		envDump += fmt.Sprintf("\n\t\t==> %s", envVar)
	}
	le.Log(ale.Debug, "Dumping Enviorment Variables: "+envDump)
}

func dumpCLIVariables(le *ale.LogEngine) {
	cliDump := ""
	for _, cliVar := range os.Args {
		cliDump += fmt.Sprintf("\n\t\t==> %s", cliVar)
	}
	le.Log(ale.Debug, "Dumping CLI Variables: "+cliDump)
}
