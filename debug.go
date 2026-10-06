package transporter

import (
	"fmt"
	"strings"

	"github.com/seanmmitchell/ale/v2"
)

// redacted replaces every value in the debug dumps below. Values may be
// secrets (tokens, passwords), so they are never written to the log.
const redacted = "<redacted>"

// dumpEnvironmentVariables logs, at Debug level, the names of the environment
// variables that carry prefix. Values are never logged and variables without
// the prefix are not mentioned at all. Names are %q-quoted because ale does
// not sanitize messages, so a crafted name cannot inject fake log lines.
func dumpEnvironmentVariables(le *ale.LogEngine, environ []string, prefix string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Dumping Environment Variables (prefix %q, values redacted):", prefix)
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		fmt.Fprintf(&b, "\n\t\t==> %q=%s", name, redacted)
	}
	le.Log(ale.Debug, b.String())
}

// cliToken is the parser's view of one CLI argument, for dumpCLIVariables.
// flag is set only for tokens the parser identified as a known flag (without
// any "=value") or the "--" terminator; everything else stays zero.
type cliToken struct {
	flag   string
	inline bool // the flag carried an "=value"
}

// dumpCLIVariables logs, at Debug level, the shape of the CLI arguments as the
// parser saw them: known flags and "--" are printed, inline values become
// "=<redacted>", and every other token (values, positionals, unknown flags,
// anything after "--") is printed as <redacted>. Unknown flags are redacted
// because the parser cannot tell them from values such as "-s3cret".
//
// Printed names are %q-quoted to prevent log injection.
func dumpCLIVariables(le *ale.LogEngine, tokens []cliToken) {
	var b strings.Builder
	b.WriteString("Dumping CLI Arguments (values redacted):")
	for _, tok := range tokens {
		switch {
		case tok.flag == "":
			b.WriteString("\n\t\t==> " + redacted)
		case tok.inline:
			fmt.Fprintf(&b, "\n\t\t==> %q=%s", tok.flag, redacted)
		default:
			fmt.Fprintf(&b, "\n\t\t==> %q", tok.flag)
		}
	}
	le.Log(ale.Debug, b.String())
}
