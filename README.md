# Transporter
![transporter-final-min](https://user-images.githubusercontent.com/20157708/221439905-b2a7c0b7-c6d0-4204-9f2b-d64c2531a61a.png)

A system for managing state and configurations and collecting inputs. Helps manage environment variables, command line arguments, and JSON configuration files.

Additionally, I (Sean) will say, for my use-case that using Transporter is pretty great, but the code base and testing for it is fairly bloated which is something I have been considering how I could improve on without compromising some of the functionality. If you encounter issues please report them!

## How to Install
    go get github.com/seanmmitchell/transporter/v2

## How to Use
Describe each setting once as a `PatternSequence`, then `Energize` the pattern to collect values from the config file, the environment and the CLI.

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/seanmmitchell/transporter/v2"
	"github.com/seanmmitchell/transporter/v2/jsto"
)

func main() {
	pattern := transporter.Pattern{
		Sequences: map[string]transporter.PatternSequence{
			"port": {
				Name:        "Port",
				Description: "TCP port to listen on.",
				CLIFlags:    []string{"port", "p"}, // --port 9090, --port=9090, -p 9090
				ENVVars:     []string{"PORT"},      // APP_PORT=9090
				Value:       "8080",                // default
			},
			"token": {
				Name:               "API Token",
				Description:        "Token for the upstream API.",
				CLIFlags:           []string{"token"},
				ENVVars:            []string{"TOKEN"}, // APP_TOKEN=...
				Required:           true,              // Energize fails without it
				DisablePersistence: true,              // never written to config.json
			},
		},
	}

	state, err := transporter.Energize(pattern, transporter.TransporterOptions{
		EnvironmentPrefix: "APP_",
		ConfigFileEngine:  jsto.New("config.json"),
	})
	if errors.Is(err, transporter.ErrRequiredMissing) {
		log.Fatal("set APP_TOKEN or pass --token")
	}
	if err != nil {
		log.Fatal(err)
	}

	port, err := state.Get("port")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("listening on", port)

	if err := state.Set("port", "9090"); err != nil {
		log.Fatal(err)
	}
	// Write the current values to config.json ("token" is saved as "no persistence").
	if err := state.Materialize(); err != nil {
		log.Fatal(err)
	}
}
```

| PatternSequence field | Meaning                                                                                       |
| --------------------- | --------------------------------------------------------------------------------------------- |
| `Value`               | Default before loading; current value after.                                                  |
| `CLIFlags`            | Flag names without dashes. The sequence key also works as a flag.                             |
| `ENVVars`             | Variable names without the prefix. The sequence key also works (`APP_port`).                  |
| `Required`            | `Energize` returns `ErrRequiredMissing` if the sequence still has no value after loading.      |
| `DisablePersistence`  | `Materialize` saves `"no persistence"` instead of the value; that config entry is never loaded. |
| `Name`, `Description`, `Example` | Documentation only (`Example` is not persisted).                                    |

## General Input Expectations
| Requirement Type     | Details                                                                                                                                       |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Precedence           | Default `Value` < config file < environment < CLI. A later source wins.                                                                       |
| CLI forms            | `--name value`, `--name=value`, `-name value`. `--` ends parsing. `argv[0]` is never parsed. Unknown flags are logged as a Warning and skipped. |
| Environment Prefix   | Only variables starting with the prefix (default `"T_"`) are read. The rest of the name is matched. Values may contain `=`.                    |
| Identifiers          | Sequence keys, CLI flags and ENV var names must be unique within their source. A duplicate makes `Energize` return `ErrDuplicateIdentifier`. |
| Config file          | Entries are matched by sequence key. A missing file is fine (it is created by `Materialize`).                                                 |

## Transporter Defaults
| Variable Name            | Default Value                      | Details                                                                                                                                                             |
| ------------------------ | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| LogEngine                | console engine, Warning and above  | Transporter relies on a simple log engine I developed known as [ALE](https://github.com/seanmmitchell/ale). See [Logging Interoperability](#logging-interoperability). |
| LogEnginePConsoleCTX     | new `pconsole.PConsoleCTX`         | Only used by the default LogEngine. In ALE, the pconsole output engine needs a CTX for write locking with front-end facing threads.                                  |
| ConfigFileLogEngine      | LogEngine                          | Log engine passed to the ConfigFileEngine (jsto).                                                                                                                   |
| EnvironmentPrefix        | `"T_"`                             | An empty prefix is reset to the default.                                                                                                                            |
| Args                     | `os.Args[1:]` (when nil)           | CLI arguments to parse. Handy for tests.                                                                                                                            |
| Environ                  | `os.Environ()` (when nil)          | `KEY=value` entries to read. Handy for tests.                                                                                                                       |
| ConfigFileEngine         | nil                                | By default, Transporter is simply a CLI & ENV argument aggregator and state management tool. Set it (e.g. `jsto.New("config.json")`) to get a JSON configuration file. |
| DumpEnvironmentVariables | false                              | Logs the names of prefixed variables at Debug level. Values are redacted.                                                                                          |
| DumpCLIArguments         | false                              | Logs the CLI arguments at Debug level. Values are redacted.                                                                                                        |

## Errors
Errors are wrapped with context, so check them with `errors.Is`.

| Error                    | Returned by  | When                                                                       |
| ------------------------ | ------------ | -------------------------------------------------------------------------- |
| `ErrKeyNotFound`         | Get, Set     | The key is not in the pattern.                                             |
| `ErrNoConfigFileEngine`  | Materialize  | No ConfigFileEngine was configured.                                        |
| `ErrDuplicateIdentifier` | Energize     | A key/flag/ENV name maps to more than one sequence in the same source.     |
| `ErrRequiredMissing`     | Energize     | A `Required` sequence has no value after loading.                          |

`Energize` also fails when the config file exists but cannot be read or parsed. A missing file is not an error. A custom `ConfigFileInterface` must return an error wrapping `fs.ErrNotExist` for a missing file.

## Security Notes
- Configuration values are never logged. The Debug dumps print names only, with values shown as `<redacted>`.
- `Materialize` persists every value, including ones that came from the environment or CLI. Mark secrets with `DisablePersistence`: they are saved as `"no persistence"`, and the in-memory value stays usable.
- jsto writes the config file atomically with `0600` permissions.
- `jsto.Save` rewrites the whole file with only the pattern's keys. Other keys in the file are dropped.

## Logging Interoperability
Transporter is currently dependent on the [ALE](https://github.com/seanmmitchell/ale) logging system. By default it creates its own console engine that only prints Warning and above, so it stays quiet unless something is wrong. The config file engine (jsto) logs to `ConfigFileLogEngine`, which falls back to `LogEngine`, so one engine controls everything. To change that, pass your own engine:

```go
pCTX, _ := pconsole.New(30, 20)
le := ale.CreateLogEngine("Transporter")
le.AddLogPipeline(ale.Error, pCTX.Log) // Error+ only; use ale.Debug to see everything,
                                       // or add no pipeline at all to silence Transporter.
state, err := transporter.Energize(pattern, transporter.TransporterOptions{LogEngine: le})
```

This is something that could be modified in the future for larger support if needed.

## Migrating from v1
| v1                                                              | v2                                                                                       |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `github.com/seanmmitchell/transporter`                          | `github.com/seanmmitchell/transporter/v2`                                                |
| `EnviormentPrefix`                                              | `EnvironmentPrefix`                                                                      |
| `DumpEnvironmentVariables`, `DumpCLIArguments` (`any`)          | `bool`                                                                                   |
| `State.Active`, `State.Stored`                                  | Unexported/removed. Use `Get` and `Set`.                                                 |
| `State.Switch()`                                                | Removed.                                                                                 |
| `ConfigFilePath`, `ConfigFileLogEnginePConsoleCTX`              | Removed. Pass the path to `jsto.New(path)`. Config file logs go to `ConfigFileLogEngine`. |
| `ENVCaseSensitive`, `CaseSensitiveFlags`                        | Removed (they were never implemented).                                                   |
| `&jsto.JSONConfig{FileLock: &sync.Mutex{}, FilePath: p}`        | `jsto.New(p)`                                                                            |
| CLI parsing included `argv[0]`                                  | `os.Args[1:]`, or the new `Args` option.                                                 |
| Read `os.Environ()` directly                                    | The new `Environ` option (defaults to `os.Environ()`).                                   |
| Default log level Debug                                         | Warning.                                                                                 |
| `Energize` always returned a nil error                          | Returns errors (unreadable/corrupt config, duplicates, missing required values).         |
| `Required` was ignored                                          | Enforced with `ErrRequiredMissing`.                                                      |
| `Materialize` ignored save failures                             | Returns errors, including `ErrNoConfigFileEngine`.                                       |

## License
This work is licensed under the MIT License.  
Please review [LICENSE](LICENSE.md) (LICENSE.md) for specifics.
