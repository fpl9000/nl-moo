# nl-moo

A multi-user MOO server where object behavior is specified in natural language rather than code.

NL-MOO is a LambdaMOO-inspired virtual world. Players connect over telnet, walk between rooms and handle objects much as in a classic MOO, but an object's methods are written in English. When a method runs, an LLM interprets it and proposes its effects as a structured document, and deterministic Go code checks that document against the method's declared contract and the world's permission rules before anything is committed. Correctness rests on the Go code, not on the model.

## Status

This repository is at milestone **M0**: a skeleton with a buildable `nl-moo` binary that parses its flags and prints its version, plus continuous integration on Linux and Windows. There is no server yet. The milestones that build it are listed in the plan below.

## Documents

- [`nl-moo-implementation-plan.md`](nl-moo-implementation-plan.md) is the plan of record: sixteen milestones, M0 to M15, each ending in a pull request. Changes to the plan land as edits to that file.
- [`docs/decisions.md`](docs/decisions.md) is a running log of decisions made along the way, including answers to the specification gaps the plan lists.
- The design itself is [`llm-moo-design-v2.md`](https://github.com/fpl9000/ai-docs/blob/main/llm-moo-design-v2.md) in the `fpl9000/ai-docs` repository. Section references such as §7.4 in the plan point into it.

## Building and testing

You need Go 1.27 or later. NL-MOO is pure Go and needs no C compiler, so the same commands work on Windows and Linux. From the repository root:

```
go build ./cmd/nl-moo
./nl-moo -version
go test ./...
```

On Windows the binary is `nl-moo.exe`, run as `.\nl-moo.exe -version` in PowerShell or `cmd`.

On Linux, `go test -race ./...` also runs the tests under Go's data-race detector, which is what CI does. On Windows the race detector needs a C toolchain, so CI runs the tests there without it.

The version reported by `-version` is `dev` unless a release version is set at build time, for example `go build -ldflags "-X main.version=v0.1.0" ./cmd/nl-moo`. When the binary is built from a git checkout, the version line also shows the commit it was built from.

## License

NL-MOO is free software, licensed under the GNU Affero General Public License, version 3 (AGPLv3). See [`LICENSE`](LICENSE). Because of the AGPL's section 13, anyone who runs a modified version of this server for others to use over a network must offer those users the modified source code.
