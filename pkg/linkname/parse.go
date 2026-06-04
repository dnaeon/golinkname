// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.
//
// Portions of this file (specifically the trailing "//"-comment trim rule
// applied to a //go:linkname directive line) are derived from
// gopls/internal/golang/linkname.go in golang.org/x/tools. The original
// upstream license is preserved verbatim above and the full upstream license
// text is reproduced in the LICENSE-third-party file.

package linkname

import "strings"

// directivePrefix is the exact comment-text prefix that opens a linkname
// directive. The compiler accepts no leading spaces between // and "go:".
const directivePrefix = "//go:linkname"

// parsed is the result of parseDirective. localName is the first argument
// (declared symbol on this side of the alias). target is the second
// argument string, empty for the one-argument form.
type parsed struct {
	form      Form
	localName string
	targetRaw string
	pkgPath   string
	name      string
	warnings  []string
	// ok reports whether the comment is a //go:linkname directive at all.
	// A non-directive line returns ok=false and zero parsed.
	ok bool
}

// parseDirective parses a single source line that may be a //go:linkname
// directive. The input must be the full comment text including the leading
// "//". Lines that do not begin with "//go:linkname" return ok=false.
//
// Recognized forms:
//
//	//go:linkname localname                    (one-arg)
//	//go:linkname localname pkgpath.name       (two-arg)
//	//go:linkname localname symname            (two-arg-extern; cgo,
//	                                            sanitizer hooks, FIPS info)
//
// The trailing-tail trim mirrors gopls (linkname.go): if a stray // appears
// later on the line, everything from there is dropped and the remainder is
// re-trimmed. This handles `//go:linkname f pkg.g //@hover(...)` style.
//
// Malformed inputs (wrong arg count, leading/trailing dot in target) still
// return ok=true with form set conservatively and a warning recorded;
// callers should attach the warnings to the emitted Record.
func parseDirective(line string) parsed {
	if !strings.HasPrefix(line, directivePrefix) {
		return parsed{}
	}
	// Boundary check: the prefix must be followed by a space, end-of-line,
	// or comment tail; otherwise this is some other directive that happens
	// to start with the same characters (e.g. //go:linknameFoo, hypothetical).
	if len(line) > len(directivePrefix) {
		if c := line[len(directivePrefix)]; c != ' ' && c != '\t' && c != '/' {
			return parsed{}
		}
	}

	directive := line
	if i := strings.LastIndex(directive, "//"); i != 0 {
		directive = strings.TrimSpace(directive[:i])
	}

	parts := strings.Fields(directive)
	switch len(parts) {
	case 2:
		return parsed{
			form:      FormOneArg,
			localName: parts[1],
			ok:        true,
		}
	case 3:
		p := parsed{
			localName: parts[1],
			targetRaw: parts[2],
			ok:        true,
		}
		dot := strings.LastIndexByte(parts[2], '.')
		switch {
		case dot < 0:
			// No dot: bare linker symbol (cgo C symbol, TSAN/libfuzzer
			// hook, FIPS info identifier). Well-formed; no pkg.name to
			// extract, no warning to emit.
			p.form = FormTwoArgExtern
		case dot == 0 || dot == len(parts[2])-1:
			// Leading or trailing dot: genuinely malformed.
			p.form = FormTwoArg
			p.warnings = append(p.warnings, WarnMalformedDirective)
		default:
			p.form = FormTwoArg
			p.pkgPath = parts[2][:dot]
			p.name = parts[2][dot+1:]
		}
		return p
	default:
		// Includes the "//go:linkname" with no arguments case (parts == 1)
		// and any 4+-token form. Both are malformed.
		return parsed{
			ok:       true,
			warnings: []string{WarnMalformedDirective},
		}
	}
}
