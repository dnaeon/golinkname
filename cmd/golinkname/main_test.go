// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

// wantFileName is the golden-output sentinel inside library txtar
// fixtures; the CLI tests skip it when materializing the source tree.
const wantFileName = "want.json"

// runCLI invokes the CLI command tree in-process with the given argv
// (without the program name). Any non-nil error from cmd.Run is treated
// as exit code 1, mirroring main()'s 0/1 contract.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := newCommand(&outBuf, &errBuf)
	err := cmd.Run(context.Background(), append([]string{"golinkname"}, args...))
	if err != nil {
		fmt.Fprintln(&errBuf, err)
		code = 1
	}
	return outBuf.String(), errBuf.String(), code
}

// extractTxtar materializes the file tree contained in a txtar archive into
// dst. It skips the want.json sentinel used by the library tests, since the
// CLI tests don't compare against it.
func extractTxtar(t *testing.T, fixturePath, dst string) {
	t.Helper()
	archive, err := txtar.ParseFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range archive.Files {
		if f.Name == wantFileName {
			continue
		}
		path := filepath.Join(dst, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// extractFixture extracts a named txtar fixture from pkg/linkname/testdata
// to a fresh temp directory.
func extractFixture(t *testing.T, name string) string {
	t.Helper()
	tmp := t.TempDir()
	extractTxtar(t, filepath.Join("..", "..", "pkg", "linkname", "testdata", name), tmp)
	return tmp
}

func TestCLI_NoArgs(t *testing.T) {
	// urfave/cli prints help to stdout and exits 0 when invoked with no
	// args. That matches the convention of `go`, `git`, etc.
	stdout, _, code := runCLI(t)
	if code != 0 {
		t.Fatalf("no-args: exit=%d, want 0", code)
	}
	if !strings.Contains(stdout, "golinkname") {
		t.Errorf("no-args: stdout missing program name; got %q", stdout)
	}
}

func TestCLI_Help(t *testing.T) {
	stdout, _, code := runCLI(t, "--help")
	if code != 0 {
		t.Fatalf("--help: exit=%d, want 0", code)
	}
	for _, want := range []string{"index", "refs", "related", "list"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--help: stdout missing %q; got %q", want, stdout)
		}
	}
}

func TestCLI_SubcommandHelp(t *testing.T) {
	stdout, _, code := runCLI(t, "refs", "--help")
	if code != 0 {
		t.Fatalf("refs --help: exit=%d, want 0", code)
	}
	if !strings.Contains(stdout, "<pkgpath>.<name>") {
		t.Errorf("refs --help: missing args usage; got %q", stdout)
	}
}

func TestCLI_UnknownFlag(t *testing.T) {
	_, stderr, code := runCLI(t, "index", "--frob")
	if code != 1 {
		t.Fatalf("unknown flag: exit=%d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("unknown flag: expected message on stderr")
	}
}

func TestCLI_Index(t *testing.T) {
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "index", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0]["form"] != "two-arg" {
		t.Errorf("form=%v, want two-arg", got[0]["form"])
	}
}

func TestCLI_Index_DashCAlias(t *testing.T) {
	// `-C` is the alias for --dir, mirroring the `go` and `git` toolchains.
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "index", "-C", dir)
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "two-arg") {
		t.Errorf("-C alias: stdout did not include record; got %q", stdout)
	}
}

func TestCLI_IndexPretty(t *testing.T) {
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "index", "--dir", dir, "--pretty")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "\n  ") {
		t.Errorf("--pretty did not indent; got %q", stdout)
	}
}

func TestCLI_Index_FileFilter_Match(t *testing.T) {
	// `index --file' restricts JSON output to records whose `file' field
	// matches exactly. The paired-push-pull fixture has directives in two
	// files; filtering to one returns just that file's record.
	dir := extractFixture(t, "paired-push-pull.txtar")
	stdout, stderr, code := runCLI(t, "index", "--dir", dir, "--file", "lower/lower.go")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0]["file"] != "lower/lower.go" {
		t.Errorf("file=%v, want lower/lower.go", got[0]["file"])
	}
}

func TestCLI_Index_FileFilter_NoMatch(t *testing.T) {
	// A non-matching path returns an empty JSON array, not an error --
	// "no directives in this file" is a real answer.
	dir := extractFixture(t, "paired-push-pull.txtar")
	stdout, stderr, code := runCLI(t, "index", "--dir", dir, "--file", "nonexistent.go")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "[]" {
		t.Errorf("no-match: stdout=%q, want []", got)
	}
}

func TestCLI_Refs_FlagAfterPositional(t *testing.T) {
	// urfave/cli supports interspersed flags, unlike stdlib `flag`. This
	// is one of the reasons we ported.
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "refs", "--dir", dir, "example.com/m/b.bar", "--pretty")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "\n  ") {
		t.Errorf("--pretty after positional did not indent; got %q", stdout)
	}
}

func TestCLI_Refs_Match(t *testing.T) {
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "refs", "--dir", dir, "example.com/m/b.bar")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d refs, want 1", len(got))
	}
}

func TestCLI_Refs_NoMatch(t *testing.T) {
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "refs", "--dir", dir, "example.com/m/b.nope")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "[]" {
		t.Errorf("no-match: stdout=%q, want []", got)
	}
}

func TestCLI_Refs_BadQuery(t *testing.T) {
	dir := extractFixture(t, "two-arg-in-module.txtar")
	for _, q := range []string{"nodot", ".leading", "trailing."} {
		_, _, code := runCLI(t, "refs", "--dir", dir, q)
		if code != 1 {
			t.Errorf("query %q: exit=%d, want 1", q, code)
		}
	}
}

func TestCLI_Refs_WrongArity(t *testing.T) {
	_, _, code := runCLI(t, "refs")
	if code != 1 {
		t.Fatalf("missing arg: exit=%d, want 1", code)
	}
}

func TestCLI_Related_BothDirections(t *testing.T) {
	// `related' is symmetric: standing on either side of a paired push/pull
	// bridge should surface both directives. The paired-push-pull fixture
	// has a Case 2 pull at upper.g and a Case 3 push at lower.f targeting
	// upper.g. Querying for upper.g returns both.
	dir := extractFixture(t, "paired-push-pull.txtar")
	stdout, stderr, code := runCLI(t, "related", "--dir", dir, "example.com/m/upper.g")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(got) != 2 {
		t.Fatalf("got %d related, want 2 (both ends of the bridge)\n%s", len(got), stdout)
	}
	files := map[string]bool{}
	for _, r := range got {
		files[r["file"].(string)] = true
	}
	if !files["upper/upper.go"] || !files["lower/lower.go"] {
		t.Errorf("missing one side of the bridge; got files=%v", files)
	}
}

func TestCLI_Related_QueryOwnLocal(t *testing.T) {
	// Querying by the local-side qualified name (`example.com/m/lower.f')
	// must also return the directive on lower.go -- this is the case where
	// the user is standing on the directive's own decl, not its target.
	dir := extractFixture(t, "paired-push-pull.txtar")
	stdout, stderr, code := runCLI(t, "related", "--dir", dir, "example.com/m/lower.f")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d related, want 1 (the directive at point)\n%s", len(got), stdout)
	}
	if got[0]["file"] != "lower/lower.go" {
		t.Errorf("got file=%v, want lower/lower.go", got[0]["file"])
	}
}

func TestCLI_Related_NoMatch(t *testing.T) {
	dir := extractFixture(t, "paired-push-pull.txtar")
	stdout, stderr, code := runCLI(t, "related", "--dir", dir, "example.com/m/upper.nope")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "[]" {
		t.Errorf("no-match: stdout=%q, want []", got)
	}
}

func TestCLI_Related_BadQuery(t *testing.T) {
	dir := extractFixture(t, "paired-push-pull.txtar")
	for _, q := range []string{"nodot", ".leading", "trailing."} {
		_, _, code := runCLI(t, "related", "--dir", dir, q)
		if code != 1 {
			t.Errorf("query %q: exit=%d, want 1", q, code)
		}
	}
}

func TestCLI_Related_WrongArity(t *testing.T) {
	_, _, code := runCLI(t, "related")
	if code != 1 {
		t.Fatalf("missing arg: exit=%d, want 1", code)
	}
}

func TestCLI_List(t *testing.T) {
	dir := extractFixture(t, "two-arg-in-module.txtar")
	stdout, stderr, code := runCLI(t, "list", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	// `func/two-arg' is the combined kind/form rendering: a separate
	// KIND column would push the table past most terminal widths, so
	// the FORM column carries both. The header is unchanged ("FORM").
	for _, want := range []string{"FILE:LINE", "a/a.go:5", "example.com/m/b.bar", "func/two-arg"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("list: missing %q; got %q", want, stdout)
		}
	}
}

func TestCLI_List_FileFilter(t *testing.T) {
	dir := extractFixture(t, "paired-push-pull.txtar")
	stdout, stderr, code := runCLI(t, "list", "--dir", dir, "--file", "nonexistent.go")
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 1 {
		t.Errorf("file-filter: got %d lines, want 1 (header only)\n%s", len(lines), stdout)
	}
}

func TestCLI_NoModule(t *testing.T) {
	dir := t.TempDir() // no go.mod here
	_, stderr, code := runCLI(t, "index", "--dir", dir)
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected error on stderr")
	}
}
