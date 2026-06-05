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
	// recvType is the receiver type name for method targets of the form
	// `pkg.(Recv).Method` or `pkg.(*Recv).Method'. The leading "*" is
	// stripped; recvType holds just the bare type identifier (e.g.
	// "rtype"). Empty for non-method targets.
	recvType string
	warnings []string
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
//	//go:linkname localname symname            (two-arg-extern; cgo, sanitizer hooks, FIPS info)
//
// The trailing-tail trim mirrors gopls (linkname.go): if a stray // appears
// later on the line, everything from there is dropped and the remainder is
// re-trimmed. This handles `//go:linkname f pkg.g //@hover(...)` style.
//
// Malformed inputs (wrong arg count, leading/trailing dot in target) still
// return ok=true with form set conservatively and a warning recorded; callers
// should attach the warnings to the emitted Record.
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
		if pkg, recv, name, ok := splitMethodTarget(parts[2]); ok {
			p.form = FormTwoArg
			p.pkgPath = pkg
			p.recvType = recv
			p.name = name
			return p
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

// splitMethodTarget recognizes the method-on-receiver target forms:
//
//	pkg.(Recv).Method
//	pkg.(*Recv).Method
//	pkg.Recv.Method      // value receiver, no parens (stdlib-only convention)
//
// On match it returns (pkgPath, receiverTypeName, methodName, true). The
// leading "*" on the receiver, if any, is stripped from receiverTypeName.  On
// non-match it returns ("", "", "", false), letting the caller fall back to the
// simple last-dot split.
//
// The stdlib uses the parenthesized form heavily in reflect/badlinkname.go to
// alias methods on unexported types (e.g.  //go:linkname x
// reflect.(*rtype).Align), since the compiler refuses //go:linkname directives
// placed *on* methods directly. The bare-receiver form (pkg.Recv.Method)
// appears in net/http and time -- it is unambiguous because Go import paths use
// "/" as a separator, so the segment between the last two dots cannot legally
// be a package-path component.
//
// The target syntax is human-readable convention -- the compiler stores the
// second argument as an opaque linker symbol -- so recognition here is purely a
// navigation aid.
func splitMethodTarget(s string) (string, string, string, bool) {
	if pkg, recv, method, ok := splitParenMethodTarget(s); ok {
		return pkg, recv, method, true
	}
	return splitBareMethodTarget(s)
}

// splitParenMethodTarget handles `pkg.(Recv).Method' and `pkg.(*Recv).Method'.
func splitParenMethodTarget(s string) (string, string, string, bool) {
	open := strings.IndexByte(s, '(')
	if open < 2 || s[open-1] != '.' {
		return "", "", "", false
	}
	close := strings.IndexByte(s[open:], ')')
	if close < 0 {
		return "", "", "", false
	}
	close += open
	// After ')' we expect ".Method" with at least one identifier char.
	if close+2 >= len(s) || s[close+1] != '.' {
		return "", "", "", false
	}
	pkg := s[:open-1]
	recv := strings.TrimPrefix(s[open+1:close], "*")
	method := s[close+2:]
	if pkg == "" || !isGoIdent(recv) || !isGoIdent(method) {
		return "", "", "", false
	}
	return pkg, recv, method, true
}

// splitBareMethodTarget handles `pkg.Recv.Method' (value receiver, no
// parens). The discriminator vs a plain `pkgpath.name' target is that
// the segment between the last two dots must be a Go identifier --
// which excludes import-path segments containing "/".
func splitBareMethodTarget(s string) (string, string, string, bool) {
	last := strings.LastIndexByte(s, '.')
	if last < 0 {
		return "", "", "", false
	}
	prev := strings.LastIndexByte(s[:last], '.')
	if prev < 0 {
		return "", "", "", false
	}
	pkg := s[:prev]
	recv := s[prev+1 : last]
	method := s[last+1:]
	if pkg == "" || !isGoIdent(recv) || !isGoIdent(method) {
		return "", "", "", false
	}
	return pkg, recv, method, true
}

// isGoIdent reports whether s is a non-empty Go identifier (ASCII-only;
// good enough for the stdlib symbols we navigate, which are ASCII by
// convention).
func isGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c == '_':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
