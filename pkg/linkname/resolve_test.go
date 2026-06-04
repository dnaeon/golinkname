// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.21\n")
	mustMkdir(t, filepath.Join(tmp, "internal", "bar"))
	mustWrite(t, filepath.Join(tmp, "internal", "bar", "bar.go"), `package bar

func Bar() {}

var V int

func (r recv) method() {}

type recv struct{}
`)
	mustWrite(t, filepath.Join(tmp, "internal", "bar", "bar_linux.go"), `//go:build linux

package bar

func Linux() {}
`)
	mustWrite(t, filepath.Join(tmp, "internal", "bar", "bar_darwin.go"), `//go:build darwin

package bar

func Linux() {}
`)
	mustWrite(t, filepath.Join(tmp, "root.go"), `package m

func RootSym() {}
`)

	m, err := FindModule(tmp)
	if err != nil {
		t.Fatal(err)
	}
	r := newResolver(m)

	t.Run("in-module func", func(t *testing.T) {
		got := r.resolve("example.com/m/internal/bar", "Bar")
		if len(got) != 1 {
			t.Fatalf("got %d, want 1: %+v", len(got), got)
		}
		if got[0].File != "internal/bar/bar.go" || !got[0].InModule {
			t.Errorf("loc = %+v", got[0])
		}
	})

	t.Run("in-module var", func(t *testing.T) {
		got := r.resolve("example.com/m/internal/bar", "V")
		if len(got) != 1 || got[0].File != "internal/bar/bar.go" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("module root package", func(t *testing.T) {
		got := r.resolve("example.com/m", "RootSym")
		if len(got) != 1 || got[0].File != "root.go" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("multiple build-tag variants", func(t *testing.T) {
		// Linux is declared in two build-tagged files; both should appear
		// because we don't evaluate build tags.
		got := r.resolve("example.com/m/internal/bar", "Linux")
		if len(got) != 2 {
			t.Errorf("expected 2 candidates for build-tag-gated symbol, got %d: %+v", len(got), got)
		}
	})

	t.Run("methods are not matched", func(t *testing.T) {
		// "method" is only defined as a method on recv; a top-level
		// linkname target with the same name should not match it.
		got := r.resolve("example.com/m/internal/bar", "method")
		if len(got) != 0 {
			t.Errorf("methods should not resolve as top-level, got %+v", got)
		}
	})

	t.Run("stdlib unresolved", func(t *testing.T) {
		got := r.resolve("runtime", "gopark")
		if len(got) != 0 {
			t.Errorf("stdlib should be unresolved, got %+v", got)
		}
	})

	t.Run("external dep unresolved", func(t *testing.T) {
		got := r.resolve("github.com/somebody/somelib/pkg", "Sym")
		if len(got) != 0 {
			t.Errorf("external dep should be unresolved, got %+v", got)
		}
	})

	t.Run("unknown name in known package", func(t *testing.T) {
		got := r.resolve("example.com/m/internal/bar", "NonExistent")
		if len(got) != 0 {
			t.Errorf("unknown name should yield empty, got %+v", got)
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		if got := r.resolve("", "Bar"); len(got) != 0 {
			t.Errorf("empty pkgPath should yield nil, got %+v", got)
		}
		if got := r.resolve("example.com/m/internal/bar", ""); len(got) != 0 {
			t.Errorf("empty name should yield nil, got %+v", got)
		}
	})
}
