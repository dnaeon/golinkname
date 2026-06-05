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
		got := r.resolve("example.com/m/internal/bar", "Bar", "")
		if len(got) != 1 {
			t.Fatalf("got %d, want 1: %+v", len(got), got)
		}
		if got[0].File != "internal/bar/bar.go" || !got[0].InModule {
			t.Errorf("loc = %+v", got[0])
		}
	})

	t.Run("in-module var", func(t *testing.T) {
		got := r.resolve("example.com/m/internal/bar", "V", "")
		if len(got) != 1 || got[0].File != "internal/bar/bar.go" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("module root package", func(t *testing.T) {
		got := r.resolve("example.com/m", "RootSym", "")
		if len(got) != 1 || got[0].File != "root.go" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("multiple build-tag variants", func(t *testing.T) {
		// Linux is declared in two build-tagged files; both should appear
		// because we don't evaluate build tags.
		got := r.resolve("example.com/m/internal/bar", "Linux", "")
		if len(got) != 2 {
			t.Errorf("expected 2 candidates for build-tag-gated symbol, got %d: %+v", len(got), got)
		}
	})

	t.Run("methods are not matched without recvType", func(t *testing.T) {
		// "method" is only defined as a method on recv; a top-level
		// linkname target with the same name should not match it.
		got := r.resolve("example.com/m/internal/bar", "method", "")
		if len(got) != 0 {
			t.Errorf("methods should not resolve as top-level, got %+v", got)
		}
	})

	t.Run("method resolves with recvType", func(t *testing.T) {
		// pkg.(recv).method form: resolver should match the method on
		// the named receiver type.
		got := r.resolve("example.com/m/internal/bar", "method", "recv")
		if len(got) != 1 {
			t.Fatalf("expected 1 method candidate, got %d: %+v", len(got), got)
		}
		if got[0].File != "internal/bar/bar.go" {
			t.Errorf("loc = %+v", got[0])
		}
	})

	t.Run("recvType narrows away free function", func(t *testing.T) {
		// Bar is a free function; with a non-empty recvType the resolver
		// must not return it.
		got := r.resolve("example.com/m/internal/bar", "Bar", "recv")
		if len(got) != 0 {
			t.Errorf("recvType should exclude free functions, got %+v", got)
		}
	})

	t.Run("stdlib unresolved", func(t *testing.T) {
		got := r.resolve("runtime", "gopark", "")
		if len(got) != 0 {
			t.Errorf("stdlib should be unresolved, got %+v", got)
		}
	})

	t.Run("external dep unresolved", func(t *testing.T) {
		got := r.resolve("github.com/somebody/somelib/pkg", "Sym", "")
		if len(got) != 0 {
			t.Errorf("external dep should be unresolved, got %+v", got)
		}
	})

	t.Run("unknown name in known package", func(t *testing.T) {
		got := r.resolve("example.com/m/internal/bar", "NonExistent", "")
		if len(got) != 0 {
			t.Errorf("unknown name should yield empty, got %+v", got)
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		if got := r.resolve("", "Bar", ""); len(got) != 0 {
			t.Errorf("empty pkgPath should yield nil, got %+v", got)
		}
		if got := r.resolve("example.com/m/internal/bar", "", ""); len(got) != 0 {
			t.Errorf("empty name should yield nil, got %+v", got)
		}
	})
}

// TestResolveBlackBoxTestPackageExcluded covers the resolver's package-name
// filter. The same directory may legitimately contain two packages -- a
// foo package and its `foo_test' black-box test variant -- and a target
// pkgPath="foo" must only resolve against `package foo' files.
//
// Concrete failure mode this prevents: the stdlib's time/linkname_test.go
// is `package time_test' and contains bodyless decls (`func absClock(...)')
// that are themselves the *pull* side of a //go:linkname. Without the
// filter, a directive targeting `time.absClock' from a third package
// would falsely "resolve" to one of those bodyless test-file decls
// instead of returning empty (or, in a fixed world, the real impl in
// `package time').
func TestResolveBlackBoxTestPackageExcluded(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.21\n")
	mustMkdir(t, filepath.Join(tmp, "foo"))

	// Production file: package foo, real implementation.
	mustWrite(t, filepath.Join(tmp, "foo", "foo.go"), `package foo

func Impl() {}
`)
	// Black-box test file in the same directory: package foo_test,
	// declares a same-named bodyless symbol that is the linkname pull
	// slot. Must NOT be returned for a target with pkgPath="foo".
	mustWrite(t, filepath.Join(tmp, "foo", "foo_test.go"), `package foo_test

import _ "unsafe"

//go:linkname Impl example.com/m/foo.Impl
func Impl()
`)

	m, err := FindModule(tmp)
	if err != nil {
		t.Fatal(err)
	}
	r := newResolver(m)

	got := r.resolve("example.com/m/foo", "Impl", "")
	if len(got) != 1 {
		t.Fatalf("expected exactly one resolution (the real impl), got %d: %+v", len(got), got)
	}
	if got[0].File != "foo/foo.go" {
		t.Errorf("expected resolution in foo/foo.go (package foo), got %+v", got[0])
	}
}

// TestResolveWhiteboxTestFileMatched is the inverse of the black-box
// test: a `_test.go' file declared as `package foo' (same package, just
// a whitebox test) IS a legitimate location for a linkname-targetable
// decl. The filter is by package name, not filename suffix, so these
// must continue to resolve.
func TestResolveWhiteboxTestFileMatched(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.21\n")
	mustMkdir(t, filepath.Join(tmp, "foo"))
	mustWrite(t, filepath.Join(tmp, "foo", "helper_test.go"), `package foo

func HelperImpl() {}
`)

	m, err := FindModule(tmp)
	if err != nil {
		t.Fatal(err)
	}
	r := newResolver(m)

	got := r.resolve("example.com/m/foo", "HelperImpl", "")
	if len(got) != 1 {
		t.Fatalf("whitebox _test.go should resolve, got %d: %+v", len(got), got)
	}
	if got[0].File != "foo/helper_test.go" {
		t.Errorf("loc = %+v", got[0])
	}
}

func TestLastPathSegment(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"time", "time"},
		{"internal/synctest", "synctest"},
		{"example.com/m/foo", "foo"},
		{"crypto/internal/fips140", "fips140"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := lastPathSegment(tt.in); got != tt.want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
