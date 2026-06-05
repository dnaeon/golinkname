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

func TestScanFile(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantLen int
		check   func(t *testing.T, recs []Record)
	}{
		{
			name: "two-arg func directive",
			src: `package p
import _ "unsafe"

//go:linkname foo example.com/m/pkg.Bar
func foo() string
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				r := recs[0]
				if r.Form != FormTwoArg {
					t.Errorf("form = %v, want two-arg", r.Form)
				}
				if r.LocalName != "foo" {
					t.Errorf("localName = %q, want foo", r.LocalName)
				}
				if r.DeclName != "foo" || r.DeclKind != DeclFunc {
					t.Errorf("decl = %s/%s, want foo/func", r.DeclName, r.DeclKind)
				}
				if r.Target == nil || r.Target.PkgPath != "example.com/m/pkg" || r.Target.Name != "Bar" {
					t.Errorf("target = %+v", r.Target)
				}
				if !r.HasUnsafeImport {
					t.Error("HasUnsafeImport should be true")
				}
				if len(r.Warnings) != 0 {
					t.Errorf("unexpected warnings: %v", r.Warnings)
				}
			},
		},
		{
			name: "one-arg directive",
			src: `package p
import _ "unsafe"

//go:linkname foo
func foo() string { return "" }
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if recs[0].Form != FormOneArg {
					t.Errorf("form = %v, want one-arg", recs[0].Form)
				}
				if recs[0].Target != nil {
					t.Errorf("Target should be nil for one-arg, got %+v", recs[0].Target)
				}
			},
		},
		{
			name: "missing unsafe import yields warning",
			src: `package p

//go:linkname foo example.com/m/pkg.Bar
func foo() string
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if recs[0].HasUnsafeImport {
					t.Error("HasUnsafeImport should be false")
				}
				if !slices.Contains(recs[0].Warnings, WarnMissingUnsafeImport) {
					t.Errorf("warnings = %v, want missing-unsafe-import", recs[0].Warnings)
				}
			},
		},
		{
			name: "plain unsafe import counts",
			src: `package p
import "unsafe"

var _ unsafe.Pointer

//go:linkname foo example.com/m/pkg.Bar
func foo() string
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if !recs[0].HasUnsafeImport {
					t.Error("plain `import \"unsafe\"` should satisfy the requirement")
				}
				if slices.Contains(recs[0].Warnings, WarnMissingUnsafeImport) {
					t.Errorf("unexpected missing-unsafe warning: %v", recs[0].Warnings)
				}
			},
		},
		{
			name: "var declaration carries directive",
			src: `package p
import _ "unsafe"

//go:linkname foo example.com/m/pkg.Bar
var foo string
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if recs[0].DeclKind != DeclVar {
					t.Errorf("declKind = %v, want var", recs[0].DeclKind)
				}
				if recs[0].DeclName != "foo" {
					t.Errorf("declName = %q, want foo", recs[0].DeclName)
				}
			},
		},
		{
			name: "multiple directives on one decl",
			src: `package p
import _ "unsafe"

//go:linkname foo example.com/m/pkg.Bar
//go:linkname bar example.com/m/pkg.Baz
func foo() string
`,
			wantLen: 2,
			check: func(t *testing.T, recs []Record) {
				if recs[0].LocalName != "foo" || recs[1].LocalName != "bar" {
					t.Errorf("localNames = %s, %s", recs[0].LocalName, recs[1].LocalName)
				}
			},
		},
		{
			name: "type and const decls are ignored",
			src: `package p
import _ "unsafe"

//go:linkname foo example.com/m/pkg.Bar
type T int

//go:linkname bar example.com/m/pkg.Baz
const C = 1
`,
			wantLen: 0,
		},
		{
			name: "nested funcs are not scanned",
			src: `package p
import _ "unsafe"

func outer() {
	//go:linkname inner example.com/m/pkg.Bar
	inner := func() {}
	_ = inner
}
`,
			wantLen: 0,
		},
		{
			name: "malformed directive yields warning",
			src: `package p
import _ "unsafe"

//go:linkname foo .runtime
func foo() string
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if !slices.Contains(recs[0].Warnings, WarnMalformedDirective) {
					t.Errorf("warnings = %v, want malformed-directive", recs[0].Warnings)
				}
			},
		},
		{
			name: "two-arg-extern directive (cgo bare symbol) does not warn",
			src: `package p
import _ "unsafe"

//go:linkname _cgo_mmap _cgo_mmap
var _cgo_mmap unsafe.Pointer
`,
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if recs[0].Form != FormTwoArgExtern {
					t.Errorf("form = %q, want %q", recs[0].Form, FormTwoArgExtern)
				}
				if slices.Contains(recs[0].Warnings, WarnMalformedDirective) {
					t.Errorf("warnings = %v, want no malformed-directive", recs[0].Warnings)
				}
				if recs[0].Target == nil || recs[0].Target.Raw != "_cgo_mmap" {
					t.Errorf("target = %#v, want raw=%q", recs[0].Target, "_cgo_mmap")
				}
			},
		},
		{
			name: "parse error in a file with a directive emits a parseError record",
			// The directive substring is present, so the
			// pre-filter does not short-circuit and the parser
			// runs. The parser fails, and we surface that --
			// without it, the user has no signal that a real
			// directive was lost to a parse failure.
			src:     "package p\n//go:linkname foo bar\nthis is not valid Go",
			wantLen: 1,
			check: func(t *testing.T, recs []Record) {
				if recs[0].ParseError == "" {
					t.Errorf("expected parseError, got %+v", recs[0])
				}
				if recs[0].Form != "" {
					t.Errorf("error record should not have Form set: %+v", recs[0])
				}
			},
		},
		{
			name: "file without a directive substring is not parsed at all",
			// No "//go:linkname" bytes in the file, so the
			// pre-filter short-circuits before invoking the
			// parser. The result: zero records, regardless of
			// whether the file would have parsed cleanly. A
			// parse error here is a non-event because we never
			// look at it.
			src:     "package p\nthis is not valid Go",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "x.go")
			if err := os.WriteFile(path, []byte(tt.src), 0o644); err != nil {
				t.Fatal(err)
			}
			recs := scanFile(path, "x.go")
			if len(recs) != tt.wantLen {
				t.Fatalf("got %d records, want %d:\n%+v", len(recs), tt.wantLen, recs)
			}
			if tt.check != nil {
				tt.check(t, recs)
			}
		})
	}
}

// TestScanFileUnreadable covers the read-failure path: when we cannot
// read the file at all, we cannot confirm the directive substring is
// present, so the contract forbids emitting a parseError record.
func TestScanFileUnreadable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.go")
	recs := scanFile(missing, "does-not-exist.go")
	if recs != nil {
		t.Fatalf("expected nil for unreadable file, got %+v", recs)
	}
}

// TestDirectionFor covers every (Form, has-body) combination so that
// the push/pull contract documented on Direction stays honest. The
// table mirrors the four cases from docs/linkname-cases.md plus the
// FormTwoArgExtern always-pull and FormOneArg/FormTwoArg-on-var
// always-pull cases (callers pass hasBody=false for vars).
func TestDirectionFor(t *testing.T) {
	tests := []struct {
		form    Form
		hasBody bool
		want    Direction
	}{
		{FormOneArg, true, DirectionPush},        // Case 1
		{FormOneArg, false, DirectionPull},       // Case 2
		{FormTwoArg, true, DirectionPush},        // Case 3
		{FormTwoArg, false, DirectionPull},       // Case 4
		{FormTwoArgExtern, true, DirectionPull},  // extern always pulls
		{FormTwoArgExtern, false, DirectionPull}, // extern always pulls
	}
	for _, tt := range tests {
		got := directionFor(tt.form, tt.hasBody)
		if got != tt.want {
			t.Errorf("directionFor(%q, hasBody=%v) = %q, want %q",
				tt.form, tt.hasBody, got, tt.want)
		}
	}
}

// TestScanFileDirection runs the full scanFile path on small fixtures
// to confirm the Direction propagated from directionFor matches the
// shape of the source. This is end-to-end coverage on top of
// TestDirectionFor's unit table.
func TestScanFileDirection(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want Direction
	}{
		{
			name: "one-arg with body is push",
			src: `package p
import _ "unsafe"

//go:linkname foo
func foo() string { return "" }
`,
			want: DirectionPush,
		},
		{
			name: "one-arg bodyless is pull",
			src: `package p
import _ "unsafe"

//go:linkname foo
func foo() string
`,
			want: DirectionPull,
		},
		{
			name: "two-arg with body is push",
			src: `package p
import _ "unsafe"

//go:linkname foo other/pkg.Foo
func foo() string { return "" }
`,
			want: DirectionPush,
		},
		{
			name: "two-arg bodyless is pull",
			src: `package p
import _ "unsafe"

//go:linkname foo other/pkg.Foo
func foo() string
`,
			want: DirectionPull,
		},
		{
			name: "two-arg-extern even with body is pull",
			src: `package p
import _ "unsafe"

//go:linkname foo bareSym
func foo() string { return "" }
`,
			want: DirectionPull,
		},
		{
			name: "var-targeted directive is pull",
			src: `package p
import _ "unsafe"

//go:linkname x other/pkg.X
var x int
`,
			want: DirectionPull,
		},
		{
			name: "one-arg var with initializer is push",
			src: `package p
import _ "unsafe"

//go:linkname drivers
var drivers = make(map[string]int)
`,
			want: DirectionPush,
		},
		{
			name: "two-arg var with initializer is push",
			src: `package p
import _ "unsafe"

//go:linkname x other/pkg.X
var x = 42
`,
			want: DirectionPush,
		},
		{
			name: "var inside block, bodyless, is pull",
			src: `package p
import _ "unsafe"

var (
	//go:linkname zeroVal runtime.zeroVal
	zeroVal [1024]byte
)
`,
			want: DirectionPull,
		},
		{
			name: "var inside block with initializer is push",
			src: `package p
import _ "unsafe"

var (
	//go:linkname x other/pkg.X
	x = 42
)
`,
			want: DirectionPush,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "x.go")
			if err := os.WriteFile(path, []byte(tt.src), 0o644); err != nil {
				t.Fatal(err)
			}
			recs := scanFile(path, "x.go")
			if len(recs) != 1 {
				t.Fatalf("got %d records, want 1: %+v", len(recs), recs)
			}
			if recs[0].Direction != tt.want {
				t.Errorf("Direction = %q, want %q (record: %+v)",
					recs[0].Direction, tt.want, recs[0])
			}
		})
	}
}
