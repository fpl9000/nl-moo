// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package main

// Go note: a file whose name ends in _test.go is compiled only by "go test",
// never into the nl-moo binary. Because it declares the same package (main),
// its tests can call unexported functions such as run directly.

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

// TestRun drives run through a few command lines and checks the exit status
// and output of each. It is written in Go's table-driven style: each case is
// one row in a slice of anonymous structs, and the loop runs every row as a
// named subtest (t.Run), so a failure report names the case that failed.
func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStatus int
		wantStdout string // substring expected on stdout ("" means stdout must be empty)
		wantStderr string // substring expected on stderr ("" means no check)
	}{
		{
			name:       "version flag prints version on stdout",
			args:       []string{"-version"},
			wantStatus: 0,
			wantStdout: "nl-moo " + version + " (",
		},
		{
			name:       "double-dash spelling also works",
			args:       []string{"--version"},
			wantStatus: 0,
			wantStdout: runtime.Version(),
		},
		{
			name:       "no flags reports the skeleton on stderr",
			args:       nil,
			wantStatus: 0,
			wantStderr: "not implemented yet",
		},
		{
			name:       "help is not an error",
			args:       []string{"-h"},
			wantStatus: 0,
			wantStderr: "Usage: nl-moo",
		},
		{
			name:       "unknown flag is a usage error",
			args:       []string{"-bogus"},
			wantStatus: 2,
			wantStderr: "flag provided but not defined",
		},
		{
			name:       "positional argument is a usage error",
			args:       []string{"serve"},
			wantStatus: 2,
			wantStderr: `unexpected argument "serve"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// bytes.Buffer satisfies io.Writer, so it can stand in for
			// os.Stdout and os.Stderr and capture what run writes.
			var stdout, stderr bytes.Buffer
			status := run(tc.args, &stdout, &stderr)

			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d (stderr: %q)", status, tc.wantStatus, stderr.String())
			}
			if tc.wantStdout == "" {
				if stdout.Len() != 0 {
					t.Errorf("stdout = %q, want it empty", stdout.String())
				}
			} else if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tc.wantStdout)
			}
			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}
