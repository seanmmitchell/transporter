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
		// Only fixed flag names, env names and argument indexes may be logged,
		// so the marker in any message means some value leaked (including
		// values later overridden, which Get no longer returns).
		if checkLeaks {
			for _, m := range msgs {
				if strings.Contains(m, fuzzMarker) {
					t.Fatalf("a value leaked into a log message: %q", m)
				}
			}
		}
		values := map[string]string{}
		for _, key := range []string{keyFirst, keyLast, keyAge} {
			v, err := s.Get(key)
			if err != nil {
				t.Fatalf("Get(%q): %v", key, err)
			}
			values[key] = v
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

// FuzzIdentifiers checks Energize's identifier rules against an independent
// model: invalid or colliding identifiers are rejected with the matching
// sentinel error, and valid ones are accepted and let the CLI set the sequence
// (overriding the environment).
func FuzzIdentifiers(f *testing.F) {
	f.Add("port", "p", "PORT")
	f.Add("a", "a", "A")
	f.Add("", "-x", "B=1")
	f.Add("user-age", "user-age", "")
	f.Add("k", "o", "OTHER")
	f.Add("o", "x", "X")
	f.Add("OTHER", "x", "X")

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

		// The model. CLI names: keys plus CLIFlags; ENV names: keys plus
		// ENVVars; "other" owns "other", "o" (CLI) and "OTHER" (ENV).
		invalid := flag == "" || strings.Contains(flag, "=") || strings.HasPrefix(flag, "-") ||
			env == "" || strings.Contains(env, "=")
		duplicate := flag == "o" || flag == "other" || env == "OTHER" || env == "other" ||
			key == "o" || key == "OTHER"

		s, err := transporter.Energize(p, o)
		switch {
		case invalid:
			if !errors.Is(err, transporter.ErrInvalidIdentifier) && !(duplicate && errors.Is(err, transporter.ErrDuplicateIdentifier)) {
				t.Fatalf("invalid identifier (flag %q, env %q): err = %v, want ErrInvalidIdentifier", flag, env, err)
			}
		case duplicate:
			if !errors.Is(err, transporter.ErrDuplicateIdentifier) {
				t.Fatalf("colliding identifier (key %q, flag %q, env %q): err = %v, want ErrDuplicateIdentifier", key, flag, env, err)
			}
		default:
			if err != nil {
				t.Fatalf("valid identifiers (key %q, flag %q, env %q) rejected: %v", key, flag, env, err)
			}
			if got, err := s.Get(key); err != nil || got != "from-cli" {
				t.Fatalf("flag %q did not set %q: got %q, %v", flag, key, got, err)
			}
		}
	})
}
