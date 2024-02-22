# Transporter
![transporter-final-min](https://user-images.githubusercontent.com/20157708/221439905-b2a7c0b7-c6d0-4204-9f2b-d64c2531a61a.png)

A system for managing state and configurations and collecting inputs. Helps managed environment variables, command line arguments, and JSON/YML configuration files.

Additionally, I (Sean) will say, for my use-case that using Transporter is pretty great, but the code base and testing for it is fairly bloated which is something I have been considering how I could improve on without compromising some of the functionality. If you encounter issues please report them!

## How to Install
    go get -u github.com/seanmmitchell/transporter

## How to Use


## General Input Expectations
| Requirement Type     | Specification                               | Details                                                                                                                                                                           |
| -------------------- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| All Arguments / Keys | Must be unique                              | Matching is performed based on the uniqueness of these keys, so as to avoid potential multi-assignment of a given input, and avoid sharing sequence keys, CLI flags, or ENV vars. |
| CLI Arguments Prefix | Must be "--"                                | All command-line interface arguments must start with "--".                                                                                                                        |
| Environment Prefix   | Must be "T_" or specified to something else | The default prefix for environment variables is "T_". If a different prefix is needed, it must be explicitly specified.                                                           |

## Transporter Defaults
| Variable Name                  | Default Value    | Details                                                                                                                                                                                                  |
| ------------------------------ | ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| LogEngine                      | ale.LogEngine    | Transporter relies on a simple log engine I developed known as [ALE](https://github.com/seanmmitchell/ale).                                                                                              |
| LogEnginePConsoleCTX           | ale pconsole.CTX | Transporter uses [ALE](https://github.com/seanmmitchell/ale). In ALE, an output engine used known as pconsole requires a CTX for write locking with front-end facing threads.                            |
| ConfigFileEngine               | ale.LogEngine    | Transporter's underlying JSTO relies on a simple log engine I developed known as [ALE](https://github.com/seanmmitchell/ale).                                                                            |
| ConfigFileLogEnginePConsoleCTX | ale pconsole.CTX | Transporter uses [ALE](https://github.com/seanmmitchell/ale). In ALE, an output engine used known as pconsole requires a CTX for write locking with front-end facing threads.                            |
| EnvironmentPrefix              | string "T_"      | This can be set within transporter options but can not be "; otherwise, it will be reset to the default.                                                                                                 |
| ConfigFileEngine               | nil              | By default, Transporter is simply a CLI & ENV argument aggregator and state management tool, but by setting this in transporter options, as seen in the example, you can have a JSON configuration file. |

## Logging Interoperability
While Transporter is currently dependent on the [ALE](https://github.com/seanmmitchell/ale) logging system and will automatically implement it, you can control it in the example seen below. Additionally, you can just set it in options for a new LogEngine with no output pipeline or with a pipeline for Error+ or even Critical+ logs. This is something that could be modified in the future for larger support if needed.

## License
This work is licensed under the MIT License.  
Please review [LICENSE](LICENSE.md) (LICENSE.md) for specifics.
