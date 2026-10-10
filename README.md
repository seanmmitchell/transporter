# Transporter
![transporter-final-min](https://user-images.githubusercontent.com/20157708/221439905-b2a7c0b7-c6d0-4204-9f2b-d64c2531a61a.png)

A system for managing state and configurations and collecting inputs. Helps manage environment variables, command line arguments, and JSON configuration files.

Additionally, I (Sean) will say, for my use-case that using Transporter is pretty great, but the code base and testing for it is fairly bloated which is something I have been considering how I could improve on without compromising some of the functionality. If you encounter issues please report them!

## How to Install
    go get github.com/seanmmitchell/transporter/v2

Requires Go 1.22 or newer.

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

	state, err := transporter.Energize(pattern, transporter.Options{
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
| `Value`               | The default. `Energize` works on a copy, so read the loaded value with `State.Get`.           |
| `CLIFlags`            | Non-empty flag names without a leading `-` and without `=` (else `ErrInvalidIdentifier`); inner dashes like `user-age` are fine. A non-empty sequence key also works as a flag. |
| `ENVVars`             | Variable names without the prefix or `=`. A non-empty sequence key also works (`APP_port`).   |
| `Required`            | `Energize` returns `ErrRequiredMissing` if the sequence still has no value after loading.      |
| `DisablePersistence`  | `Materialize` saves `"no persistence"` instead of the value; that config entry is never loaded. |
| `Name`, `Description`, `Example` | Documentation only (`Example` is not persisted).                                    |

## General Input Expectations
| Requirement Type     | Details                                                                                                                                       |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Precedence           | Default `Value` < config file < environment < CLI. A later source wins.                                                                       |
| CLI forms            | `--name value`, `--name=value`, `-name value`. A value may start with `-` (`--offset -5`), but `--` is never a value: it ends parsing. `argv[0]` is never parsed. Unknown flags are skipped with a Warning that names only their position (the token may be a value). |
| Environment Prefix   | Only variables starting with the prefix (default `"T_"`) are read. The rest of the name is matched. Values may contain `=`.                    |
| Identifiers          | Sequence keys, CLI flags and ENV var names must be unique within their source. A duplicate makes `Energize` return `ErrDuplicateIdentifier`. |
| Config file          | Entries are matched by sequence key. A missing file is fine (it is created by `Materialize`). A key containing `=` can only be set from the config file. |

## Transporter Defaults
| Variable Name            | Default Value                      | Details                                                                                                                                                             |
| ------------------------ | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| LogEngine                | console engine, Warning and above  | Transporter relies on a simple log engine I developed known as [ALE](https://github.com/seanmmitchell/ale). See [Logging Interoperability](#logging-interoperability). |
| LogEnginePConsoleCTX     | new `pconsole.PConsoleCTX`         | Only used by the default LogEngine. In ALE, the pconsole output engine needs a CTX for write locking with front-end facing threads.                                  |
| ConfigFileLogEngine      | LogEngine                          | Log engine passed to the ConfigFileEngine (jsto).                                                                                                                   |
| EnvironmentPrefix        | `"T_"`                             | An empty prefix is reset to the default.                                                                                                                            |
| Args                     | `os.Args[1:]` (when nil)           | CLI arguments to parse. Only `nil` means "use the process"; pass `[]string{}` for none (a filtered slice that ends up nil falls back to `os.Args`).             |
| Environ                  | `os.Environ()` (when nil)          | `KEY=value` entries to read. As with Args, pass `[]string{}` for none.                                                                                              |
| ConfigFileEngine         | nil                                | By default, Transporter is simply a CLI & ENV argument aggregator and state management tool. Set it (e.g. `jsto.New("config.json")`) to get a JSON configuration file. |
| DumpEnvironmentVariables | false                              | Logs the names of prefixed variables at Debug level. Values are redacted. Needs a LogEngine with a Debug pipeline; the default engine hides it.                     |
| DumpCLIArguments         | false                              | Logs the CLI arguments as parsed, at Debug level: known flags by name, every other token `<redacted>`. Needs a LogEngine with a Debug pipeline; the default engine hides it. |

## Errors
Errors are wrapped with context, so check them with `errors.Is`.

| Error                    | Returned by  | When                                                                       |
| ------------------------ | ------------ | -------------------------------------------------------------------------- |
| `ErrKeyNotFound`         | Get, Set     | The key is not in the pattern.                                             |
| `ErrNoConfigFileEngine`  | Materialize  | No ConfigFileEngine was configured.                                        |
| `ErrDuplicateIdentifier` | Energize     | A key/flag/ENV name maps to more than one sequence in the same source.     |
| `ErrInvalidIdentifier`   | Energize     | A CLI flag is empty, starts with `-` or contains `=`, or an ENV name is empty or contains `=`. |
| `ErrRequiredMissing`     | Energize     | A `Required` sequence has no value after loading.                          |
| `jsto.ErrInvalidJSON`    | Load (and so Energize) | The config file is not a JSON object.                            |
| `jsto.ErrNotRegularFile` | Save (and so Materialize), Load (and so Energize) | Save: the config path is a symlink, directory or other non-regular file. Load: the path resolves to a FIFO, device or directory (a dangling link is reported as missing instead). |
| `jsto.ErrFileTooLarge`   | Load (and so Energize) | The config file is over 16 MiB.                                  |

`Energize` also fails when the config file exists but cannot be read or parsed. A missing file is not an error. A custom `ConfigFileInterface` must return an error wrapping `fs.ErrNotExist` for a missing file, and its `Save` (like any log pipeline) must not call `Materialize`, which would deadlock. Always obtain a `State` from `Energize`; a zero `State` is not usable.

## Security Notes
- Configuration values are never logged. The Debug dumps print names only, with values shown as `<redacted>`.
- `Materialize` persists every value, including ones that came from the environment or CLI. Mark secrets with `DisablePersistence`: they are saved as `"no persistence"`, and the in-memory value stays usable.
- Keep the config file in a directory only its owner can write. jsto writes it atomically with `0600` permissions: a temp file in the same directory is renamed over it, so that directory must be writable, and the file's owner becomes the writing user. On Windows `0600` does not restrict readers.
- `jsto.Save` refuses (`ErrNotRegularFile`) a config path that is a symlink, directory, device or FIFO. Following a link would let one planted in a shared directory such as `/tmp` redirect the write (e.g. to `~/.bashrc`), and replacing `/dev/null` would break the system. Point `jsto.New` at the real file instead. Only the last path element is checked: symlinked parent directories are followed.
- `jsto.Load` reads through links (on Linux, `fs.protected_symlinks` stops other users' links in sticky directories such as `/tmp`; other systems have no such check). It refuses a path that resolves to a directory, device or FIFO, checking the file it actually opened, and reads at most 16 MiB, since some special files report as regular yet never end. A dangling link counts as a missing file. If another process holds a lease on the file (e.g. a Samba oplock), `Load` waits for it to be released, up to 60 seconds.
- jsto parse errors report only a byte offset, never file content. The CLI dump prints only flags Transporter knows; values, positionals and unknown flags appear as `<redacted>`.
- `jsto.Save` rewrites the whole file with only the pattern's keys. Other keys in the file are dropped.
- Values must be valid UTF-8: invalid bytes come back as U+FFFD after a save and reload (standard JSON encoding).

## Logging Interoperability
Transporter is currently dependent on the [ALE](https://github.com/seanmmitchell/ale) logging system. By default it creates its own console engine that only prints Warning and above, so it stays quiet unless something is wrong. The config file engine (jsto) logs to `ConfigFileLogEngine`, which falls back to `LogEngine`, so one engine controls everything. To change that, pass your own engine:

```go
package main

import (
	"log"

	"github.com/seanmmitchell/ale/v2"
	"github.com/seanmmitchell/ale/v2/pconsole"
	"github.com/seanmmitchell/transporter/v2"
)

func main() {
	pCTX, err := pconsole.New(30, 20)
	if err != nil {
		log.Fatal(err)
	}
	le := ale.CreateLogEngine("Transporter")
	// Error and above only; use ale.Debug to see everything,
	// or add no pipeline at all to silence Transporter.
	le.AddLogPipeline(ale.Error, pCTX.Log)

	pattern := transporter.Pattern{Sequences: map[string]transporter.PatternSequence{
		"port": {CLIFlags: []string{"port"}, Value: "8080"},
	}}
	state, err := transporter.Energize(pattern, transporter.Options{LogEngine: le})
	if err != nil {
		log.Fatal(err)
	}
	port, _ := state.Get("port")
	log.Println("port:", port)
}
```

Every Go example in this README is a complete program; CI builds them all (`scripts/check-readme-examples.sh`).

This is something that could be modified in the future for larger support if needed.

## Migrating from v1
| v1                                                              | v2                                                                                       |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `github.com/seanmmitchell/transporter`                          | `github.com/seanmmitchell/transporter/v2`                                                |
| Go 1.19 or newer                                                | Go 1.22 or newer.                                                                        |
| `transporter.TransporterOptions`                                | `transporter.Options`                                                                    |
| `CONF_DisablePersistence_Phrase`                                | `DisablePersistencePhrase` (same value, `"no persistence"`).                             |
| `EnviormentPrefix`                                              | `EnvironmentPrefix`                                                                      |
| `DumpEnvironmentVariables`, `DumpCLIArguments` (`any`)          | `bool`                                                                                   |
| `State.Active`, `State.Stored`                                  | Unexported/removed. Use `Get` and `Set`.                                                 |
| `State.Switch()`                                                | Removed.                                                                                 |
| `ConfigFilePath`, `ConfigFileLogEnginePConsoleCTX`              | Removed. Pass the path to `jsto.New(path)`. Config file logs go to `ConfigFileLogEngine`. |
| `ENVCaseSensitive`, `CaseSensitiveFlags`                        | Removed (they were never implemented).                                                   |
| `&jsto.JSONConfig{FileLock: &sync.Mutex{}, FilePath: p}`        | `jsto.New(p)`                                                                            |
| CLI parsing included `argv[0]`                                  | `os.Args[1:]`, or the new `Args` option.                                                 |
| Read `os.Environ()` directly                                    | The new `Environ` option (defaults to `os.Environ()`).                                   |
| Any source could match any identifier (an ENV name could hit a `CLIFlags` entry, a CLI flag an `ENVVars` entry, a config key either) | Each source matches only its own names: CLI → key + `CLIFlags`, ENV → key + `ENVVars`, config file → key. Add the missing `ENVVars`/`CLIFlags` entries. |
| `EnviormentPrefix: ""` read every unprefixed variable           | An empty prefix means `"T_"`; unprefixed variables (e.g. `PORT`) are never read.         |
| Config entries without a `DisablePersistence` field were skipped | They load (a missing field counts as `false`).                                           |
| Unknown flags and positional args were logged with their text   | Unknown flags are logged by position at Warning; positional args only at Verbose.        |
| Default log level Debug                                         | Warning.                                                                                 |
| `Energize` always returned a nil error                          | Returns errors (unreadable, corrupt or non-regular config file; duplicate or invalid identifiers; missing required values). |
| `Required` was ignored                                          | Enforced with `ErrRequiredMissing`.                                                      |
| `Materialize` ignored save failures                             | Returns errors, including `ErrNoConfigFileEngine`.                                       |
| `Energize` and `Set` wrote values into the caller's `Pattern` map | The caller's `Pattern` is copied and never changed. Read values with `Get`.            |
| A custom `ConfigFileInterface.Load` could return any error for a missing file | It must wrap `fs.ErrNotExist`, or `Energize` fails on a first run with no file yet. |
| jsto wrote through a symlinked config path                     | `Load` still reads through it, but `Save` (and so `Materialize`) returns `ErrNotRegularFile`. Point `jsto.New` at the real file. |
| jsto wrote the file in place, mode 0644                        | Writes a 0600 temp file and renames it over the config: the directory must be writable, hard links to the file break, and the file's owner becomes the writing user. |
| jsto read FIFOs, devices and files of any size                  | `Load` returns `ErrNotRegularFile` for FIFOs, devices and directories, and `ErrFileTooLarge` for files over 16 MiB. |

## License
This work is licensed under the MIT License.  
Please review [LICENSE](LICENSE.md) (LICENSE.md) for specifics.
