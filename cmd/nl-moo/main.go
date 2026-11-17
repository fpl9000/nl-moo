// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

// Command nl-moo is the NL-MOO server: a multi-user MOO in which object
// behavior is written in natural language and interpreted by an LLM, while all
// state changes are validated and committed by deterministic Go code.
//
// At milestone M0 this is only a skeleton. It parses its command-line flags
// and can print its version; the server itself arrives in later milestones
// (see nl-moo-implementation-plan.md at the top of the repository).
//
// Go note: a directory under cmd/ holding a "package main" with a main
// function is the conventional layout for a Go executable. "go build
// ./cmd/nl-moo" names the resulting binary after the directory, so it is
// nl-moo (nl-moo.exe on Windows).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
)

// version is the release version reported by -version. It defaults to "dev"
// and can be overridden at build time without editing source, using the
// linker's -X flag, which sets a package-level string variable:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/nl-moo
//
// It is a var rather than a const because -X can only set variables.
var version = "dev"

// main is deliberately tiny: it hands the real work to run, which takes its
// arguments and output streams as parameters and returns an exit status
// instead of calling os.Exit itself. That separation lets tests call run
// directly with fake arguments and in-memory buffers, which is a common Go
// pattern for testing a command's behavior without building and spawning
// the binary.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args (the command line without the program name), does what
// the flags ask, and returns the process exit status: 0 for success, 2 for a
// command-line usage error (the same status the standard flag package uses).
func run(args []string, stdout, stderr io.Writer) int {
	// A FlagSet is an independent set of flags. Using our own, rather than
	// the package-level flag.CommandLine, keeps run free of global state so
	// tests can call it repeatedly. ContinueOnError makes Parse return an
	// error instead of exiting the process, which run turns into a status.
	fs := flag.NewFlagSet("nl-moo", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// fs.Bool returns a *bool that Parse fills in. Go's flag package accepts
	// both -version and --version, so either spelling works.
	showVersion := fs.Bool("version", false, "print the version and exit")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: nl-moo [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		// flag.ErrHelp means the user asked for -h or -help; Parse has
		// already printed the usage text, so that is a success. Any other
		// error (an unknown flag, say) has also been reported by Parse.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	// Anything left after the flags is a positional argument. Nothing takes
	// one yet (subcommands such as "init" and "dump" arrive at M3), so
	// reject them rather than silently ignoring them.
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "nl-moo: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return 2
	}

	if *showVersion {
		fmt.Fprintln(stdout, versionString())
		return 0
	}

	// With no flags there is nothing to do yet, so say so plainly.
	fmt.Fprintln(stderr, versionString())
	fmt.Fprintln(stderr, "nl-moo: the server is not implemented yet; this build is the M0 skeleton.")
	return 0
}

// versionString returns a one-line description of this build, for example:
//
//	nl-moo dev (commit 1a2b3c4d5e6f, go1.27.4, windows/amd64)
//
// The commit comes from the build information that the go command embeds in
// every binary it builds from inside a git checkout (Go 1.18 and later). It
// is omitted when that information is unavailable, for example in a test
// binary. A "+dirty" suffix means the working tree had uncommitted changes.
func versionString() string {
	commit := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		dirty := false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		// A full git hash is 40 hex digits; 12 is plenty to identify it.
		if len(commit) > 12 {
			commit = commit[:12]
		}
		if commit != "" && dirty {
			commit += "+dirty"
		}
	}

	details := fmt.Sprintf("%s, %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if commit != "" {
		details = "commit " + commit + ", " + details
	}
	return fmt.Sprintf("nl-moo %s (%s)", version, details)
}
