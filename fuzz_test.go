package transporter_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/seanmmitchell/transporter/v2"
)

// fuzzMarker tags secret-like values in the seed corpus; the fuzzer mutates
// around it, and any value carrying it must never reach a log message.
const fuzzMarker = "Zq9"

// splitFuzz turns a NUL-separated fuzz string into a non-nil slice (nil would
// make Energize fall back to the real process args/environment).
func splitFuzz(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\x00")
}

// FuzzCLI feeds arbitrary argument and environment lists to Energize.
// Invariants: no error or panic for a valid pattern, every key readable,
// parsing stops at the first "--", and no value carrying fuzzMarker is logged
// (debug dumps included).
func FuzzCLI(f *testing.F) {
	f.Add("--f\x00Zq9-cli\x00--age=Zq9-eq\x00--\x00--ln\x00after", "T_FN=Zq9=env\x00T_LN=last")
	f.Add("-f\x00-Zq9\x00--nope\x00--age\x00", "T_AGE=1")
	f.Add("--tokn\x00-Zq9-after-unknown\x00-pZq9\x00Zq9-positional", "")
	f.Add("--age\x00--\x00--f\x00v", "T_\x00=x\x00T_X\nFAKE=1")

	f.Fuzz(func(t *testing.T, rawArgs, rawEnv string) {
		args, environ := splitFuzz(rawArgs), splitFuzz(rawEnv)
		energizeWith := func(args []string) (*transporter.State, []string) {
			o, logs := baseOptions()
			o.Args, o.Environ = args, environ
			o.DumpCLIArguments, o.DumpEnvironmentVariables = true, true
			s, err := transporter.Energize(newPattern(), o)
			if err != nil {
				t.Fatalf("Energize: %v", err)
			}
			return s, logs.messages()
		}

		// Environment variable names may be logged; if one carries the marker,
		// a logged name is indistinguishable from a leaked value.
		checkLeaks := true
		for _, kv := range environ {
			if name, _, _ := strings.Cut(kv, "="); strings.Contains(name, fuzzMarker) {
				checkLeaks = false
			}
		}

		s, msgs := energizeWith(args)
		values := map[string]string{}
		for _, key := range []string{keyFirst, keyLast, keyAge} {
			v, err := s.Get(key)
			if err != nil {
				t.Fatalf("Get(%q): %v", key, err)
			}
			values[key] = v
			if checkLeaks && strings.Contains(v, fuzzMarker) {
				for _, m := range msgs {
					if strings.Contains(m, v) {
						t.Fatalf("value %q of %q leaked into a log message: %q", v, key, m)
					}
				}
			}
		}

		// Everything after the first "--" is ignored.
		for i, a := range args {
			if a == "--" {
				truncated, _ := energizeWith(args[:i])
				for key, want := range values {
					if got, _ := truncated.Get(key); got != want {
						t.Fatalf("args after \"--\" changed %q: %q vs %q", key, want, got)
					}
				}
				break
			}
		}
	})
}

// FuzzIdentifiers checks that Energize either rejects an identifier with a
// sentinel error or accepts it and lets it set the sequence from the CLI
// (which overrides the environment).
func FuzzIdentifiers(f *testing.F) {
	f.Add("port", "p", "PORT")
	f.Add("a", "a", "A")
	f.Add("", "-x", "B=1")
	f.Add("user-age", "user-age", "")
	f.Add("k", "o", "OTHER")

	f.Fuzz(func(t *testing.T, key, flag, env string) {
		if key == "other" {
			t.Skip("collides with the fixed second sequence")
		}
		p := transporter.Pattern{Sequences: map[string]transporter.PatternSequence{
			key:     {CLIFlags: []string{flag}, ENVVars: []string{env}},
			"other": {CLIFlags: []string{"o"}, ENVVars: []string{"OTHER"}},
		}}
		o, _ := baseOptions()
		o.Args = []string{"--" + flag, "from-cli"}
		o.Environ = []string{transporter.DefaultEnvironmentPrefix + env + "=from-env"}

		s, err := transporter.Energize(p, o)
		if err != nil {
			if !errors.Is(err, transporter.ErrDuplicateIdentifier) && !errors.Is(err, transporter.ErrInvalidIdentifier) {
				t.Fatalf("Energize: unexpected error %v", err)
			}
			return
		}
		if got, err := s.Get(key); err != nil || got != "from-cli" {
			t.Fatalf("accepted flag %q did not set %q: got %q, %v", flag, key, got, err)
		}
	})
}
