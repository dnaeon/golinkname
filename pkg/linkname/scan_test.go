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

//go:linkname foo runtime
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
			name:    "parse error produces single error record",
			src:     `package p\nthis is not valid Go`,
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
