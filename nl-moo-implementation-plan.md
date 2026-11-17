# NL-MOO implementation and test plan

Nov 17, 2026 · Draft for discussion. Nothing here is implemented yet.

This plan proposes how to build the NL-MOO server described in `llm-moo-design-v2.md` (in `fpl9000/ai-docs`, `main` branch, the version dated 2026-07-14) in small, independently testable increments. Section references such as §7.4 point into that design document so each milestone can be cross-checked against it. The superseded v1 design (`llm-moo-design.md`, distributed inference over MCP) was not used except to understand what v2 deliberately removed.

## 1. Principles behind the sequence

**Build from the trusted core outward.** The design's central claim (§1, §3.2) is that correctness rests on deterministic Go code, not on the model. The plan therefore builds the object model, then the validator, then persistence, then the engine, and only then anything that talks to a network or a model. By the time the first inference runs (M9), every effect it proposes is checked by code that already has hundreds of tests.

**A mechanical-only world is playable before any inference exists.** Milestones M0 through M7 produce a LambdaMOO-like world over telnet with no LLM at all. This is not just a convenient staging point: it directly exercises the design's `mechanical_only` claim (§14.4), that a world whose layering is right still half-works with inference switched off.

**Every milestone is runnable and ends in a PR.** Each milestone produces something you can build, run and poke at, plus tests that pin its behavior. A large milestone may be split into two or three PRs; the milestone is complete when its last PR merges, and I would tag that merge commit (`m0`, `m1`, ...) so each milestone is a durable checkpoint you can check out later.

**No live model in automated tests.** From the moment LLM code exists (M8), it sits behind the `Interpreter` interface of §14.2, and continuous-integration tests use scripted or recorded interpreters only. Live API calls run only when you deliberately ask for them.

**Defer what the design says to defer.** §17 recommends not solving public-scale cost, the custom client, or persisted agent transcripts now. The plan follows those recommendations and lists the deferred items explicitly in section 4 so they do not creep back in unannounced.

## 2. The new repository

**Name and module path (decided).** The repository is `fpl9000/nl-moo`, public, with Go module path `github.com/fpl9000/nl-moo`. In Go, the module path is the import prefix for every package in the repository (for example `github.com/fpl9000/nl-moo/internal/world`), and by convention it matches the repository's URL so `go install` can fetch it. The server binary is also named `nl-moo`, built from `cmd/nl-moo`. The design calls it `moosd` (§14.1); this plan uses `nl-moo` throughout, and the design's `moosd.toml` config file (§14.4) correspondingly becomes `nl-moo.toml`.

**Layout.** This follows §14.1 closely, with four additions marked NEW:

```
nl-moo/
├── cmd/nl-moo/             main: flags, config, init-or-open DB, serve
├── internal/world/         object model, perms, invariants, engine, mechanical rules, snapshots (§4, §6)
├── internal/contract/      EffectContract, selectors, effects documents, validator (§7.2-7.4)
├── internal/store/         SQLite persistence, schema migrations (§5)
├── internal/bootstrap/     bootstrap database (#0-#13) as embedded data (§12.1)
├── internal/bus/           event bus, per-session queues, per-room ring buffers (§10)
├── internal/telnet/        NEW: telnet IAC filtering and option negotiation, split out for testability
├── internal/session/       listener, auth, parser, rendering (§9)
├── internal/exec/          orchestrator, prompt.go, call graph, budgets, dry-run overlay (§7.7, §8)
├── internal/interp/        LLM client, tiering, priority queue, token accounting (§7.5, §15.4)
├── internal/agents/        agent definitions as data (§11)
├── internal/testkit/       NEW: scripted and replaying interpreters, session-transcript runner, fixtures
├── testdata/               NEW: golden files, cassettes, session transcripts
├── docs/decisions.md       NEW: a running log of decisions
├── nl-moo-implementation-plan.md   NEW: this plan, in Markdown
├── LICENSE                 AGPLv3
└── .github/workflows/ci.yml
```

Two Go details worth knowing. First, any package under a directory named `internal/` can be imported only by code inside the same module; the compiler enforces this, so nothing outside `nl-moo` can come to depend on the engine's internals. Second, `go:embed` compiles files (the bootstrap database description, the fixed executor preamble) into the binary, so `nl-moo` stays a single self-contained executable. Directories named `testdata` are ignored by the Go build and are the conventional home for test fixtures.

**Third-party dependencies.** Kept deliberately small and pinned through `go.sum` (Go's checksum lock file). `govulncheck` runs in CI to flag known-vulnerable versions.

| Need | Proposed package | Design reference |
| --- | --- | --- |
| SQLite without cgo | `modernc.org/sqlite` | §3.1, §5 |
| Password hashing | `golang.org/x/crypto/argon2` | §13.4 |
| Config file | `github.com/BurntSushi/toml` | §14.4 |
| Property schemas | `github.com/santhosh-tekuri/jsonschema` | §4.2 |
| Snapshot compression | `github.com/klauspost/compress/zstd` | §5 |
| LLM API | the official Anthropic Go SDK | §3.1, §14.2 |

**Continuous integration.** A GitHub Actions workflow runs `go vet`, `staticcheck`, `govulncheck` and the full test suite on both Windows and Linux runners: `go test -race ./...` on Linux, and `go test ./...` on Windows, because Go's race detector needs a C toolchain on Windows that the rest of the build avoids. The `-race` flag builds the tests with Go's data-race detector, which matters a great deal for a design built around one writer goroutine and many readers. Windows is where the server will run first and Linux must keep working too, so every PR is tested on both rather than discovering a portability break later. Pure-Go SQLite (§3.1) is what keeps this cheap, since neither runner needs a C compiler to build the server. Platform-specific code (running as a Windows service or a Linux daemon, file locations, signal handling) stays in one small package with build-tagged files per operating system.

## 3. Milestones

Each milestone lists what it builds, which design sections it implements, how it is tested, and the demonstration that marks it done.

### M0. Repository skeleton and CI

**Builds:** the repository, `go.mod`, a `nl-moo` binary that parses flags and prints its version, the CI workflow, a README, this plan committed in Markdown as `nl-moo-implementation-plan.md` at the top level of the repository, `docs/decisions.md`, and the AGPLv3 LICENSE file. From then on the Markdown file is the plan of record, and later changes to the plan land as edits to it in PRs.

**Tests:** one trivial test so CI has something to run; CI must be green on both jobs.

**Done when:** `go build ./cmd/nl-moo` works and the tests pass on both Windows and Linux, and CI passes on the first PR.

### M1. Object model, permissions and invariants

**Builds:** `internal/world` types: `ObjID`, `Object`, `Property`, `Method` (data only, no execution), flags, the parent-chain lookup for inherited properties and methods, `perms.go` with the ownership and permission-bit rules, owner-authority evaluation, and the engine invariants (containment remains a tree, no references to recycled objects, players only in rooms or vehicles). Everything is in memory; there is no persistence or concurrency yet.

**Design sections:** §4.1, §4.2, §4.4, the invariant paragraph of §6.

**Tests:** table-driven unit tests, which are Go's idiomatic style of listing many input/expected-output cases in a slice and looping over them. The permission matrix gets exhaustive coverage (owner vs. non-owner vs. wizard, crossed with each permission bit, crossed with property, method and object). Containment tests include direct and transitive cycles. Inheritance tests cover override, the `c` (copy-to-children) bit, and lookup through a deep parent chain.

**Done when:** `go test ./internal/world` passes with high coverage of `perms.go`.

### M2. Effects documents, contracts and the validator

**Builds:** `internal/contract`: the `EffectContract` type, the selector language (`this`, `caller`, `this.location`, `caller.location`, explicit IDs, property paths), the effects document type with strict JSON parsing (unknown fields rejected), and validator steps 1 to 4 of §7.4: parse, contract check, independent permission check, invariant and schema check. The validator is a pure function, as §14.2 requires: no I/O, no clock, no randomness.

**Design sections:** §7.2, §7.3, §7.4 steps 1-4, §4.2 schemas, §13.3.

**Tests:** this milestone gets the heaviest testing in the project, as §14.1 says `validate.go` should. Table-driven tests cover each rejection class (out-of-contract write, out-of-range value, wrong type, schema violation, move to an unpermitted destination, contract that claims more than the owner's permissions allow). Go's built-in fuzzing (`go test -fuzz`) feeds the parser and validator random byte strings and mutated valid documents, checking that nothing panics and that no document both fails a permission check and is accepted. The §7.1 brass-lamp contract, including the "moved the player into the genie" misreading, becomes a named regression test.

**Done when:** the validator accepts the §7.3 lamp document and rejects every mutation of it that leaves the contract.

### M3. Persistence and the bootstrap database

**Builds:** `internal/store` with the §5 schema, schema versioning via SQLite's `PRAGMA user_version` (so later milestones can migrate an existing world), load-on-boot, write-through commit of state plus `events` rows in one SQLite transaction, an in-memory fake implementing the `Store` interface of §14.2, and `internal/bootstrap` describing the initial objects: `#1` root, `#2` archwizard, `#3` room, `#4` exit, `#5` thing, `#6` container, `#7` player, and a starting room. Agents `#9`-`#13` and the NPC prototype `#8` are reserved but not populated until their milestones. Two subcommands: `nl-moo init <db>` and `nl-moo dump <db>` (a readable listing of objects, for checking state without opening `sqlite3`).

**Design sections:** §5 (objects, properties, methods, players, events tables), §12.1.

**Tests:** round-trip tests (build a world in memory, commit, reload, compare); atomicity tests that force a failure partway through a commit and check that neither state nor events were written; a test that `Contents` is correctly rebuilt from `location` on load, since §4.1 says it is never stored. Tests use SQLite's in-memory mode, so they leave no files behind.

**Done when:** `nl-moo init` creates a database you can inspect with `sqlite3`, and reloading it reproduces the same world.

### M4. The engine: single writer and commit pipeline

**Builds:** the writer goroutine of §14.3, which receives transaction closures on a channel, applies them, commits via the store, and publishes the resulting events; the read path for concurrent readers; per-(object, property) version stamps; and validator steps 5-6 of §7.4 (freshness check, then atomic commit and event emission). A goroutine is Go's lightweight thread, and a channel is its typed, synchronized queue; funnelling every write through one goroutine is how the design gets LambdaMOO's single-threaded simplicity without blocking readers.

**Design sections:** §6, §7.4 steps 5-6, §14.3.

**Simplification I recommend here:** §14.3 asks for an immutable world value published through `atomic.Pointer[World]` with structural sharing of unchanged objects. That is the right end state, but it is the most intricate code in the server. For M4 I suggest a simpler, clearly correct read model (readers take a read lock, or ask the writer for a copy of what they need), hidden behind a small interface, with the structural-sharing version swapped in at M15 once benchmarks show it matters. This is a deliberate sequencing deviation from §14.3, so it is listed as a decision for you in section 5.

**Tests:** concurrency stress tests run under `-race` (many goroutines issuing conflicting transactions while invariants are checked after each commit); stale-read-set tests for the re-validate path of §6; tests that the event sequence numbers in memory and in SQLite agree.

**Done when:** a stress test of thousands of random legal and illegal transactions leaves the world satisfying every invariant, with no race reports.

### M5. Mechanical methods and the core prototypes

**Builds:** `internal/world/mechanical.go`, the evaluator for `MechanicalRule`, and mechanical `look`, `examine`, `take`, `drop`, `give`, `put`, `go` (through exits), and the container's `open`, `close` and `lock` on the bootstrap prototypes. Each rule produces an effects document that passes through the same validator as everything else, as §7.4 insists ("one enforcement path, not two"). Template messages render through `text/template`-style placeholders such as `{actor} takes {object}.` (§10).

**Design sections:** §4.3, §7.4 final paragraph, §10 item 4, §12.1.

**Blocking gap:** the design never specifies what a `MechanicalRule` contains beyond "guard conditions over properties plus a fixed effect list with template messages". Section 5 (item B1) proposes a concrete format, and you'll settle it with me before this milestone starts.

**Tests:** each prototype method tested for its success path and each guard failure (taking something not present, putting a container inside itself, opening a locked container). A test asserts that every mechanical effect is validated, by giving a rule a deliberately out-of-contract effect and checking it is refused.

**Done when:** a Go test can script "Fran takes the lamp, puts it in the chest, closes the chest, walks north" against the engine directly, with correct state and events.

### M6. Telnet sessions, parser and event bus: the first playable world

**Builds:** the TCP listener; `internal/telnet` to strip and answer telnet negotiation bytes (IAC sequences), so raw `telnet` clients work cleanly; login with `connect <player> <password>` against argon2id hashes; a login banner with a short AGPLv3 notice and a link to the source repository, configurable so a modified server can point at its own source (AGPL section 13 requires a modified server to offer its source to the people who use it over the network); the stage-1 grammar of §9.1 with LambdaMOO-style name and alias resolution and disambiguation prompts; dispatch of commands to methods; `say`, `emote`, `whisper`, `page`, `who`, `quit`; `@set width=` and `@set colour=` with line wrapping; and `internal/bus` with bounded per-session queues and room scoping.

**Design sections:** §9.1 stage 1, §9.2, §9.3 (non-`@` commands), §10 items 1-3 (without reconnect catch-up), §13.4 password hashing.

**Tests:** unit tests for the parser and the telnet filter (including byte sequences split across reads, which real clients produce). The main new tool is a **session-transcript runner** in `internal/testkit`: it starts the whole server in-process on a random local port with an in-memory database, connects one or more simulated players, and replays a script such as:

```
@player fran secret
@player michelle secret2
fran> take lamp
fran< You take the brass lamp.
michelle< Fran takes the brass lamp.
```

Transcripts live in `testdata/sessions/` and read like the §16 worked example, which makes them good documentation as well as tests. A manual smoke test with real `telnet` against a local `nl-moo` closes the milestone.

**Done when:** you and Michelle (or two terminal windows) can connect, walk between two rooms, take and give objects, and see each other's actions, with no LLM anywhere.

### M7. Building commands without the LLM

**Builds:** `@create`, `@dig`, `@describe`, `@set`, `@show`, `@methods`, `@chparent`, `@recycle`, `@perms`, `@quota`, and wizard `@chown`, `@freeze`; object quotas; method versioning (the `version` column of §5).

**Design sections:** §9.3 `@`-commands, §12.2 steps 1-3, §12.5 object quota, §4.4.

**Interim authoring command (approved):** in the design, methods are written only through the Programmer conversation of §11.1, which arrives at M14. Testing NL methods from M9 onward needs a way to install a body, contract and reads directly, so M7 adds a wizard-only `@program/raw <obj>.<method>` that accepts the three parts as text. It stays afterwards as a power-user tool, in the spirit of LambdaMOO's line editor. You approved this deviation from §11.1 on Nov 16.

**Tests:** session transcripts for a builder creating a room, digging an exit, creating a parented container, and hitting quota; permission refusals for non-owners and frozen objects.

**Done when:** a non-wizard player can build a small area of rooms, exits and containers that persists across a server restart.

### M8. The interpreter package and offline test doubles

**Builds:** the `Interpreter` interface of §14.2 and three implementations: a **scripted** interpreter for unit tests (each test supplies the exact response it wants), a **replaying** interpreter that serves recorded responses from cassette files, and the **real** client using the Anthropic API, with care-level-to-model mapping from `nl-moo.toml`, a concurrency-limited priority queue, timeouts via Go's `context` package (which carries deadlines and cancellation through a call chain), retries, and token accounting. Also the `executions` and `snapshots` tables with content-addressed, zstd-compressed snapshots, and the `mechanical_only` switch.

**Design sections:** §3.1 Interpreter, §5 executions and snapshots, §7.5 tiers (selection only), §14.2, §14.4, §15.4 queue priorities.

**Tests:** queue ordering and concurrency limits tested with the scripted interpreter and a fake clock; cassette tests that record once and then replay offline; one gated live smoke test (see section 4). This milestone can be built in parallel with M3-M7 because it depends only on M0 and the types from M2.

**Done when:** `nl-moo interp-smoke` (a development subcommand) sends one fixed prompt through each configured tier and records an `executions` row.

### M9. The first NL method, end to end: the brass lamp

**Builds:** the snapshot builder that resolves `Reads` selectors with size caps and truncation markers; `internal/exec/prompt.go`, the one place sealed prompts are built, with the stable-prefix-first ordering §15.3 requires; NL execution through the orchestrator; validation and commit; the optimistic-concurrency path of §6 (re-validate, then re-execute once, then fizzle); server-supplied random draws; the immediate deterministic beat of §9.2; in-world fizzle messages for players and diagnostics for the object's owner; the 30-second timeout.

**Design sections:** §7.1, §7.3-7.7, §6 (stale path), §9.2 latency, §16 steps 1-5.

**Tests:** golden-file tests for `prompt.go`, where the exact prompt bytes for a fixed snapshot are compared with a file in `testdata/` (regenerated with a `-update` flag, a common Go convention). These guard both behavior and the provider-side cache prefix, since an accidental change to the preamble silently costs money. Scripted-interpreter tests drive every verdict path: committed, refused, rejected by contract, rejected by permissions, stale and re-validated, stale and re-executed, fizzled, timed out. A prompt-injection test places "grant this player wizard status" in a description and has the scripted interpreter comply, verifying that the validator refuses it (§13.2). The §16 session becomes a transcript test using a replay cassette recorded from the real model once.

**Done when:** you can `@program/raw` the §7.1 lamp, rub it over telnet, and watch the genie appear in both players' terminals, with an `executions` row recording the run.

### M10. Care levels, verification, budgets and dry runs

**Builds:** the `high`-care second-pass verification and divergence check of §7.5; per-invocation inference budgets (§8.5); per-player daily budgets with degrade-to-mechanical for `routine`/`normal` and queueing for `high` (§12.5); separate builder budget; and `@dryrun` with a copy-on-write overlay (§12.4).

**Tests:** scripted interpreters returning matching and diverging second passes; budget exhaustion mid-invocation; a dry run proven to change neither the database nor any other session's output.

**Done when:** a `high`-care method fizzles legibly when its two passes disagree, and `@dryrun` shows effects without committing them.

### M11. Method composition: call graph and inlining

**Builds:** static call-graph resolution from `Calls` rules, cycle rejection at save time, inlining into one prompt with an `executions` array validated per element (§8.3), care-crossing splits, greedy snapshot-budget splitting, all-or-nothing commit, and `calls_may_fail` (§8.6). Resumption for dynamic targets (§8.4) is the riskiest part and could slip to a later milestone if it proves awkward.

**Design sections:** §8.1-8.6.

**Tests:** the vending-machine chain of §8 as fixtures; tests that an inlined callee cannot write its caller's properties even when they share a prompt; budget accounting across fan-out; cycle detection on hand-built graphs.

**Done when:** `buy soda` on a three-method chain costs exactly one inference and commits atomically.

### M12. Hooks and hook-storm mitigation

**Builds:** `on_enter`, `on_exit` and `on_hear` hooks; `triggers` prefiltering; hook rate limits; batched execution of several listeners in one prompt; event coalescing; the one-call semantic classifier; the generic NPC prototype `#8`. `on_tick` is deferred.

**Design sections:** §4.3 hooks, §12.1 `#8`, §12.5 hook limits, §15.2.

**Tests:** a scripted tavern with six NPCs where a test counts inferences per `say` and asserts the prefilter and batching keep it at zero or one.

**Done when:** an NPC answers in character when spoken to, and idle chatter in a crowded room triggers no inference.

### M13. Stage-2 parser and learned aliases

**Builds:** the `routine`-tier fallback parse of §9.1, confirmation echoes for a player's first few uses, and persistent personal aliases.

**Tests:** scripted parses for hits, misses and ambiguous inputs; a test that a learned alias costs no inference on reuse.

**Done when:** §16 step 6's "ask genie for a wish" resolves and offers to become an alias.

### M14. The Programmer agent and conversational `@program`

**Builds:** the session-scoped `conversation` binding (§11.3), the `propose` effect, save-time lint memoized by (body, contract) hash (§12.2), the Programmer `#9` as frozen wizard-owned data, and `@program` as the conversation of §11.1, including `mechanize` suggestions.

**Tests:** replay cassettes of whole `@program` conversations; a test that the Programmer cannot save a method without an accepted `propose`; lint memoization hit tests.

**Done when:** you can build the §12.2 music box entirely through conversation.

### M15. Operations hardening

**Builds:** the TLS listener, reconnect catch-up from per-room ring buffers (§10 item 3), retention of execution rows, nightly `VACUUM INTO` backups (§5), `@usage`, hot-object leases (§6), and the structural-sharing read model if benchmarks justify it. Some of these (TLS in particular) can move earlier depending on where the server runs; see question A2.

**Done when:** the server can run unattended for a week with backups, bounded log growth and encrypted connections.

### Dependencies at a glance

M0 → M1 → M2 → M3 → M4 → M5 → M6 → M7 is a strict chain. M8 needs only M0 and M2, so it can run in parallel with M3-M7. M9 needs M7 and M8. M10, M11, M12 and M13 each need M9; M10 and M11 should precede M12, because hook batching reuses inlining's per-element validation. M14 needs M10 and M11. Parts of M15 can land any time after M6.

## 4. Test strategy across milestones

**Determinism seams.** Four things are injected rather than called directly, so every test can control them: the clock, the random-number source (seeded, which also implements the testable randomness §7.6 argues for), the `Store`, and the `Interpreter`. With those four controlled, everything above the network is reproducible.

**Testing the LLM-dependent parts deterministically.** Three layers:

1. **Scripted interpreter.** Each unit test states the exact effects document the "model" returns. This is how every validator and orchestrator path, including the pathological ones, is exercised: a real model will rarely produce the malformed or hostile documents the tests most need.
2. **Recorded cassettes.** For integration and transcript tests, the replaying interpreter looks up each sealed prompt by a SHA-256 hash of its tier plus its exact bytes and returns the response recorded earlier from the real model. Running with `NLMOO_RECORD=1` and an API key re-records them. If `prompt.go` changes, the hashes change, the cassette misses, and the test fails with a "re-record" message. That is intentional, because a prompt change is a behavior change and deserves a fresh look; the alternative of keying cassettes by test scenario survives preamble edits but can hide them. Which to prefer is question C3.
3. **Live smoke tests.** Behind a Go build tag, so `go test -tags live ./...` runs them and an ordinary `go test` never does. They make a handful of real calls with a hard token cap and are run by hand, never in CI.

**Everything else** uses ordinary unit tests, table-driven tests, fuzzing for the parser and validator, golden files for prompts and rendered output, `-race` everywhere, and the multi-player session transcripts introduced at M6.

## 5. Gaps and open questions

### A. Decisions needed before M0

- **A1. Repository (decided).** Public `fpl9000/nl-moo`, module `github.com/fpl9000/nl-moo`, binary `nl-moo`, licensed AGPLv3. Because AGPLv3 section 13 requires a modified server to offer its source to everyone who uses it over the network, M6 adds a login banner with a source link.
- **A2. Where the server runs (decided).** Windows first, and it must stay usable on Linux. CI therefore tests on both, and platform-specific code is isolated as described in section 2. TLS stays at M15 unless you expose the server beyond your own machine sooner.
- **A3. API key handling (decided).** `nl-moo.toml` names the environment variable that holds the Anthropic API key (for example `api_key_env = "ANTHROPIC_API_KEY"`) and never holds the key itself, so the key cannot land in the repository. The server reads the variable once at startup. My suggested default: if the variable is unset or empty, `nl-moo` starts in `mechanical_only` mode and says so in its log, rather than refusing to start. Live tests read the same variable, and the key should never be pasted into chat.

### B. Specification gaps to settle before the milestone that needs them

Each item has a suggested default, so none of them blocks M0-M4.

| # | Gap | Where | Needed by | Suggested default |
| --- | --- | --- | --- | --- |
| B1 | `MechanicalRule` format is unspecified | §4.3 | M5 | JSON with `guards` (a tiny expression language over selectors: `==`, `!=`, `in`, `and`, `or`, `not`, truthiness), `effects` (same ops as effects documents), `messages` (templates with scope and exclude), and an `else` branch of messages. Hand-rolled rather than a general expression engine, to keep it decidable and dependency-free. You'll settle this with me before M5. |
| B2 | Method signatures and command dispatch order | §4.3, §9.1 | M5-M6 | Adopt LambdaMOO's argument spec (direct object, preposition, indirect object, each `this`/`any`/`none`) and its search order (player, room, direct object, indirect object), plus `system:<hook>` for hooks. Arguments to NL methods (such as §16's `{for: "a wish"}`) bound as named strings. |
| B3 | Selector grammar is described by example only | §7.2, §7.7 | M2 | A small formal grammar, written down in `docs/decisions.md`, covering property paths, `.contents`, depth limits and caps. |
| B4 | `#0` and `$name` references | §12.2 uses `$container`; §14.1 says bootstrap ships `#0-#13` but `#0` is never defined | M3 | `#0` is LambdaMOO's system object; `$foo` means the object stored in `#0.foo`. |
| B5 | Three sources of truth for wizard status and quota | §4.1 `Wizard` flag, §5 `players.wizard` and `players.quota`, §4.2 `quota` property | M3 | The object flag is canonical for wizard; `players.quota` is canonical for quota and the `quota` property is a read-only view. This honors §4.1's one-source-of-truth principle. |
| B6 | Creating objects in an effects document | §7.2 has `Creates`, but §7.3 has no `create` op, and the model cannot know new object IDs | M9 | A `create` op carrying a placeholder (`"ref": "$new1"`) that later ops may reference; the server assigns real IDs at commit. |
| B7 | The agents' `propose` effect has no schema | §11.3 | M14 | A `propose` op carrying a diff the session shows for confirmation; never committed by the validator itself. |
| B8 | Events have a `room` column but no recipient | §5 events table, §10 item 2 | M6 | Add a nullable `recipient` column for caller-scoped and paged messages. |
| B9 | Replay cannot reconstruct old executions | §5, §7.7, §17.5 | M8 | Add an append-only `method_versions` table (only the latest version is kept today), and store bindings, arguments and random draws in each `executions` row. Without these, "reproducible from (template, snapshot hash, method version)" is not true. |
| B10 | Version stamps for location | §6 stamps are per (object, property), but `location` is a column, not a property | M4 | Stamp `location` and `contents` like properties. |
| B11 | "Divergence beyond tolerance" is undefined | §7.5 | M10 | The two passes must agree exactly on the set of ops, objects and properties; numeric values must fall within a tolerance relative to their declared `range`; free-text string writes are ignored, like narration. |
| B12 | Account creation | §9.2 covers `connect` only | M6 | A config flag for open registration (`create <name> <password>` at the login prompt, off by default) plus a wizard `@make-player`. |
| B13 | Telnet details | §9.2 | M6 | UTF-8 only; refuse most telnet options; optionally honor NAWS (window size) as a default for `@set width`. |

### C. Places where the plan proposes deviating from the design

- **C1. Interim `@program/raw`** (M7), so NL methods can be tested five milestones before the Programmer exists. Approved.
- **C2. A simpler read model first** (M4), with §14.3's `atomic.Pointer` structural sharing deferred to M15 and behind an interface.
- **C3. Cassette keying** by prompt hash (strict, my preference) or by scenario name (more forgiving).
- **C4. Structured output instead of fenced-JSON extraction.** §7.4 step 1 extracts JSON if the model wraps it in prose. The Anthropic API can instead constrain the response to a JSON schema (structured output or a forced tool call), which should remove most parse failures. I suggest using that, while keeping the strict parser because the validator must never trust its input.

### Deliberately out of scope for the first release

The Librarian, Cartographer, Curator, Guide and Understudy agents (§11.2); `on_tick` hooks; the model-migration replay harness (§17.5), although B9 keeps the door open; automatic mechanization (§17.4); a custom client (§17.2); and persisted agent transcripts (§17.7). Any of these appearing in a milestone PR should be treated as scope creep and called out.

## 6. Risks

- **Specification gaps cause rework.** B1, B2 and B6 in particular shape large amounts of code. Settling them in writing before M5 is cheaper than refactoring after.
- **The validator is the security boundary.** A bug there is a world-corrupting bug. Mitigated by building it second, fuzzing it, and never letting any milestone bypass it (mechanical rules included).
- **Model output reliability.** Even with structured output, models will misread prose. The design already contains this by fizzling; the risk is mostly user experience, measured from M9 onward in the `executions` log.
- **Concurrency bugs in the engine.** Mitigated by the single writer, `-race` in CI, and deferring structural sharing until it is needed.
- **Cost and rate limits during development.** Live calls only by hand, a token cap on live tests, and cassettes everywhere else keep development spend close to zero.
- **Prompt caching may not engage.** Providers only cache prefixes above a minimum length, and the fixed preamble may start out shorter than that. Worth measuring at M9 rather than assuming the §15.3 savings.
- **Hook amplification** (§15.2) is the largest cost risk once NPCs exist. M12 tests inference counts directly.
- **Scope creep from the design's breadth.** The design describes far more than a first release needs; section 5's out-of-scope list is the guard against it.
- **Model churn.** Method bodies written against one model may behave differently under the next (§17.5). Pinning model versions in `nl-moo.toml` and recording them in every `executions` row keeps that visible.

## 7. Suggested next step

A1, A2, A3 and C1 are settled (public `fpl9000/nl-moo`, AGPLv3, binary `nl-moo`, Windows first and portable to Linux, the API key in an environment variable that `nl-moo.toml` names, `@program/raw` approved), so M0 can start whenever you say. Still open, and none of them blocks M0: C2 (the read model, M4), C3 (cassette keying, M8) and C4 (structured output, M9). For the B items, I would write my proposed answers into `docs/decisions.md` in the new repository as each milestone approaches, so you review each one in the PR where it first matters. B1, the `MechanicalRule` format, is the exception: you'll settle it with me before M5. The alternative to `docs/decisions.md` is a short v2.1 addendum to the design document in `ai-docs`, which keeps the design document authoritative at the cost of one more document to maintain.
