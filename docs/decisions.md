# Decisions

A running log of decisions about NL-MOO's implementation, newest milestone last. Each entry says what was decided, when, and why, and points to the plan item (in [`nl-moo-implementation-plan.md`](../nl-moo-implementation-plan.md)) or design section (in [`llm-moo-design-v2.md`](https://github.com/fpl9000/ai-docs/blob/main/llm-moo-design-v2.md)) it settles. Proposed answers to the plan's section 5 specification gaps are added here in the pull request of the milestone that first needs them, so each one is reviewed where it matters.

## Settled before M0

- **Repository and naming (plan A1), Nov 16, 2026.** The code lives in the public repository `fpl9000/nl-moo` with Go module path `github.com/fpl9000/nl-moo`. The executable is `nl-moo`, built from `cmd/nl-moo`, rather than the design's `moosd` (§14.1), and the design's `moosd.toml` config file (§14.4) correspondingly becomes `nl-moo.toml`.
- **License (plan A1), Nov 16, 2026.** AGPLv3. Section 13 of that license obliges anyone running a modified server for network users to offer them its source, so M6 adds a login banner with a configurable source link.
- **Platforms (plan A2), Nov 16, 2026.** Windows first, and the server must keep working on Linux. CI therefore tests every change on both, and platform-specific code will be confined to one small package with build-tagged files per operating system. TLS stays at M15 unless the server is exposed beyond the local machine sooner.
- **API key handling (plan A3), Nov 16, 2026.** `nl-moo.toml` names the environment variable that holds the Anthropic API key (for example `api_key_env = "ANTHROPIC_API_KEY"`) and never holds the key itself, so the key cannot be committed to the repository. The plan suggests that an unset or empty variable starts the server in `mechanical_only` mode with a log message rather than refusing to start; that detail is implemented, and can be revisited, at M8.
- **Interim `@program/raw` (plan C1), Nov 16, 2026.** A wizard-only `@program/raw <obj>.<method>` command arrives at M7, so natural-language methods can be installed and tested before the Programmer agent exists (M14). It stays afterwards as a power-user tool. This is an approved deviation from §11.1.

## M0

- **Go version.** `go.mod` requires Go 1.27, the current stable release. CI installs the newest 1.27.x patch release on every run, so standard-library security fixes, which `govulncheck` would otherwise flag, arrive without a code change. Moving to a newer Go release is a deliberate one-line change to `go.mod` in its own PR.
- **License file.** The repository was created with the GPLv3 text in `LICENSE`. M0 replaces it with the verbatim AGPLv3 text to match the A1 decision above.
- **Pinned analysis tools.** CI installs fixed versions of `staticcheck` and `govulncheck` (set at the top of `.github/workflows/ci.yml`), so a new release of either tool cannot turn CI red unannounced; upgrades happen in a PR.
- **Version reporting.** `nl-moo -version` prints a version string (`dev` unless set at build time with `-ldflags "-X main.version=..."`), the git commit the binary was built from when the go command recorded one, the Go version, and the target OS and architecture. The commit makes every build traceable to a checkpoint.
