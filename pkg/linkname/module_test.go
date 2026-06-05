// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFindModule(t *testing.T) {
	tmp := t.TempDir()

	// Build a layout:
	//   tmp/go.mod                (module example.com/outer)
	//   tmp/sub/go.mod            (module example.com/inner)  -- separate module
	//   tmp/pkg/foo.go            (no go.mod here, walks up)
	//   tmp/sub/pkg/bar.go
	mustWrite(t, filepath.Join(tmp, "go.mod"), "module example.com/outer\n\ngo 1.21\n")
	mustMkdir(t, filepath.Join(tmp, "sub"))
	mustWrite(t, filepath.Join(tmp, "sub", "go.mod"), "module example.com/inner\n\ngo 1.21\n")
	mustMkdir(t, filepath.Join(tmp, "pkg"))
	mustMkdir(t, filepath.Join(tmp, "sub", "pkg"))

	t.Run("from module root", func(t *testing.T) {
		m, err := FindModule(tmp)
		if err != nil {
			t.Fatal(err)
		}
		if m.Path != "example.com/outer" {
			t.Errorf("got module path %q, want example.com/outer", m.Path)
		}
	})

	t.Run("from nested dir walks up", func(t *testing.T) {
		m, err := FindModule(filepath.Join(tmp, "pkg"))
		if err != nil {
			t.Fatal(err)
		}
		if m.Path != "example.com/outer" {
			t.Errorf("got module path %q, want example.com/outer", m.Path)
		}
	})

	t.Run("nested module wins from inside it", func(t *testing.T) {
		m, err := FindModule(filepath.Join(tmp, "sub", "pkg"))
		if err != nil {
			t.Fatal(err)
		}
		if m.Path != "example.com/inner" {
			t.Errorf("got module path %q, want example.com/inner", m.Path)
		}
	})

	t.Run("no enclosing module", func(t *testing.T) {
		// Use [os.TempDir]'s parent -- root or near-root, won't have go.mod.
		bare := t.TempDir()
		// Don't create go.mod; just walk from a subdir that doesn't exist.
		nonexistent := filepath.Join(bare, "nope")
		mustMkdir(t, nonexistent)
		if _, err := FindModule(nonexistent); err == nil {
			t.Error("expected error for module-less directory, got nil")
		}
	})
}

func TestWalkGoFiles(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.21\n")

	// Files we expect to find:
	mustWrite(t, filepath.Join(tmp, "a.go"), "package m\n")
	mustMkdir(t, filepath.Join(tmp, "internal"))
	mustWrite(t, filepath.Join(tmp, "internal", "b.go"), "package internal\n")
	mustWrite(t, filepath.Join(tmp, "internal", "b_test.go"), "package internal\n") // _test.go included

	// Files we should skip:
	mustMkdir(t, filepath.Join(tmp, "vendor", "x"))
	mustWrite(t, filepath.Join(tmp, "vendor", "x", "v.go"), "package x\n")

	mustMkdir(t, filepath.Join(tmp, "testdata"))
	mustWrite(t, filepath.Join(tmp, "testdata", "td.go"), "package td\n")

	mustMkdir(t, filepath.Join(tmp, ".hidden"))
	mustWrite(t, filepath.Join(tmp, ".hidden", "h.go"), "package h\n")

	mustMkdir(t, filepath.Join(tmp, "_underscore"))
	mustWrite(t, filepath.Join(tmp, "_underscore", "u.go"), "package u\n")

	// Nested submodule: its files should be skipped entirely.
	mustMkdir(t, filepath.Join(tmp, "sub"))
	mustWrite(t, filepath.Join(tmp, "sub", "go.mod"), "module example.com/sub\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(tmp, "sub", "s.go"), "package sub\n")

	// Non-.go file should not appear.
	mustWrite(t, filepath.Join(tmp, "README.md"), "")

	m, err := FindModule(tmp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.WalkGoFiles()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"a.go",
		"internal/b.go",
		"internal/b_test.go",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("WalkGoFiles\n got:  %v\n want: %v", got, want)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
