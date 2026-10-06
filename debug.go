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

// dumpCLIVariables logs, at Debug level, the shape of the CLI arguments with
// every value redacted:
//   - flag tokens (leading "-") are printed as-is, except that the value of
//     "--name=value" / "-name=value" is redacted;
//   - every other token is redacted, as is the token right after a bare flag
//     (it is that flag's value, even when it starts with "-");
//   - "--" is always printed as-is, and everything after it is redacted.
//
// Printed names are %q-quoted to prevent log injection.
func dumpCLIVariables(le *ale.LogEngine, args []string) {
	var b strings.Builder
	b.WriteString("Dumping CLI Arguments (values redacted):")
	expectValue := false
	terminated := false
	for _, arg := range args {
		switch {
		case terminated:
			b.WriteString("\n\t\t==> " + redacted)
		case arg == "--":
			fmt.Fprintf(&b, "\n\t\t==> %q", arg)
			terminated = true
		case expectValue:
			b.WriteString("\n\t\t==> " + redacted)
			expectValue = false
		case strings.HasPrefix(arg, "-"):
			if name, _, hasValue := strings.Cut(arg, "="); hasValue {
				fmt.Fprintf(&b, "\n\t\t==> %q=%s", name, redacted)
			} else {
				fmt.Fprintf(&b, "\n\t\t==> %q", arg)
				expectValue = true
			}
		default:
			b.WriteString("\n\t\t==> " + redacted)
		}
	}
	le.Log(ale.Debug, b.String())
}
