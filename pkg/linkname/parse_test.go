// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"reflect"
	"testing"
)

func TestParseDirective(t *testing.T) {
	tests := []struct {
		name string
		line string
		want parsed
	}{
		{
			name: "not a directive",
			line: "// just a comment",
			want: parsed{},
		},
		{
			name: "non-linkname pragma",
			line: "//go:noinline",
			want: parsed{},
		},
		{
			name: "two-arg basic",
			line: "//go:linkname foo example.com/m/pkg.Bar",
			want: parsed{
				form:      FormTwoArg,
				localName: "foo",
				targetRaw: "example.com/m/pkg.Bar",
				pkgPath:   "example.com/m/pkg",
				name:      "Bar",
				ok:        true,
			},
		},
		{
			name: "two-arg with deeply nested pkg path",
			line: "//go:linkname f a/b/c/d.Sym",
			want: parsed{
				form:      FormTwoArg,
				localName: "f",
				targetRaw: "a/b/c/d.Sym",
				pkgPath:   "a/b/c/d",
				name:      "Sym",
				ok:        true,
			},
		},
		{
			name: "one-arg",
			line: "//go:linkname foo",
			want: parsed{
				form:      FormOneArg,
				localName: "foo",
				ok:        true,
			},
		},
		{
			name: "zero-arg malformed",
			line: "//go:linkname",
			want: parsed{
				ok:       true,
				warnings: []string{WarnMalformedDirective},
			},
		},
		{
			name: "four-token malformed",
			line: "//go:linkname a b c",
			want: parsed{
				ok:       true,
				warnings: []string{WarnMalformedDirective},
			},
		},
		{
			name: "two-arg target without dot is extern",
			line: "//go:linkname foo runtime",
			want: parsed{
				form:      FormTwoArgExtern,
				localName: "foo",
				targetRaw: "runtime",
				ok:        true,
			},
		},
		{
			name: "two-arg target with colon-prefixed magic is extern",
			line: "//go:linkname Linkinfo go:fipsinfo",
			want: parsed{
				form:      FormTwoArgExtern,
				localName: "Linkinfo",
				targetRaw: "go:fipsinfo",
				ok:        true,
			},
		},
		{
			name: "two-arg target with leading dot is malformed",
			line: "//go:linkname foo .Bar",
			want: parsed{
				form:      FormTwoArg,
				localName: "foo",
				targetRaw: ".Bar",
				ok:        true,
				warnings:  []string{WarnMalformedDirective},
			},
		},
		{
			name: "two-arg target with trailing dot is malformed",
			line: "//go:linkname foo runtime.",
			want: parsed{
				form:      FormTwoArg,
				localName: "foo",
				targetRaw: "runtime.",
				ok:        true,
				warnings:  []string{WarnMalformedDirective},
			},
		},
		{
			name: "trailing comment tail is trimmed (gopls compat)",
			line: "//go:linkname foo runtime.Bar // some note",
			want: parsed{
				form:      FormTwoArg,
				localName: "foo",
				targetRaw: "runtime.Bar",
				pkgPath:   "runtime",
				name:      "Bar",
				ok:        true,
			},
		},
		{
			name: "trailing test marker is trimmed",
			line: "//go:linkname foo mod.com/lower.bar //@hover(\"x\",\"x\",bar)",
			want: parsed{
				form:      FormTwoArg,
				localName: "foo",
				targetRaw: "mod.com/lower.bar",
				pkgPath:   "mod.com/lower",
				name:      "bar",
				ok:        true,
			},
		},
		{
			name: "tab separated args",
			line: "//go:linkname\tfoo\truntime.Bar",
			want: parsed{
				form:      FormTwoArg,
				localName: "foo",
				targetRaw: "runtime.Bar",
				pkgPath:   "runtime",
				name:      "Bar",
				ok:        true,
			},
		},
		{
			name: "lookalike non-directive (linknameFoo)",
			line: "//go:linknameFoo bar",
			want: parsed{},
		},
		{
			name: "method target with pointer receiver",
			line: "//go:linkname x reflect.(*rtype).Align",
			want: parsed{
				form:      FormTwoArg,
				localName: "x",
				targetRaw: "reflect.(*rtype).Align",
				pkgPath:   "reflect",
				name:      "Align",
				recvType:  "rtype",
				ok:        true,
			},
		},
		{
			name: "method target with value receiver",
			line: "//go:linkname x time.(Time).abs",
			want: parsed{
				form:      FormTwoArg,
				localName: "x",
				targetRaw: "time.(Time).abs",
				pkgPath:   "time",
				name:      "abs",
				recvType:  "Time",
				ok:        true,
			},
		},
		{
			name: "method target with nested pkg path",
			line: "//go:linkname x example.com/m/internal/x.(*T).M",
			want: parsed{
				form:      FormTwoArg,
				localName: "x",
				targetRaw: "example.com/m/internal/x.(*T).M",
				pkgPath:   "example.com/m/internal/x",
				name:      "M",
				recvType:  "T",
				ok:        true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDirective(tt.line)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDirective(%q):\n got:  %+v\n want: %+v", tt.line, got, tt.want)
			}
		})
	}
}
