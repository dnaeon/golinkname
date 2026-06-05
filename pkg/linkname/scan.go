// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
)

// linknameNeedle is the byte sequence every //go:linkname directive contains,
// derived from [directivePrefix] so the two cannot drift.  Files that do not
// contain this byte sequence cannot carry a directive and need not be
// parsed. The match is intentionally permissive (it accepts
// e.g. //go:linknameFoo, "//go:linkname" inside a string literal): false
// positives just trigger an unnecessary parse, which is harmless. False
// negatives would drop directives, so the needle must be a strict subset of
// what [parseDirective] recognizes -- which is why it is taken from
// [directivePrefix] verbatim.
var linknameNeedle = []byte(directivePrefix)

// scanFile parses a single Go source file and returns one Record per
// //go:linkname directive observed on a top-level [ast.FuncDecl] or [ast.GenDecl].
//
// relPath is the module-relative, slash-separated path used as [Record.File].
// absPath is the on-disk path passed to the parser.
//
// Files that do not contain the byte sequence "//go:linkname" are
// short-circuited: the parser is not invoked and scanFile returns nil.  On the
// Go stdlib this elides >95% of files. A parse error in such a file is a
// non-event -- we never read it as Go source -- so no record is produced. If
// the file cannot be read at all, scanFile likewise returns nil: we cannot
// confirm the substring is present, so the contract forbids emitting a
// parseError record.
//
// If the directive substring IS present and the parser fails, scanFile returns
// a single Record with ParseError set (no other fields except File and
// SchemaVersion). The directive existed but could not be extracted, and the
// user needs to know.
//
// The Record.Target.Resolved slice is left empty; resolution happens in
// resolve.go. The missing-unsafe-import warning is added here based on
// file-level state.
func scanFile(absPath, relPath string) []Record {
	src, err := os.ReadFile(absPath)
	if err != nil || !bytes.Contains(src, linknameNeedle) {
		return nil
	}

	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, absPath, src,
		parser.ParseComments|parser.SkipObjectResolution)
	if perr != nil {
		return []Record{{
			SchemaVersion: SchemaVersion,
			File:          relPath,
			ParseError:    perr.Error(),
		}}
	}

	hasUnsafe := fileImportsUnsafe(file)

	var out []Record
	emit := func(doc *ast.CommentGroup, declName string, declKind DeclKind, hasBody bool) {
		if doc == nil {
			return
		}
		for _, c := range doc.List {
			p := parseDirective(c.Text)
			if !p.ok {
				continue
			}
			pos := fset.Position(c.Pos())
			rec := Record{
				SchemaVersion:   SchemaVersion,
				File:            relPath,
				Line:            pos.Line,
				Col:             pos.Column,
				Form:            p.form,
				Direction:       directionFor(p.form, hasBody),
				LocalName:       p.localName,
				DeclName:        declName,
				DeclKind:        declKind,
				HasUnsafeImport: hasUnsafe,
				Warnings:        []string{},
			}
			if p.form == FormTwoArg || p.form == FormTwoArgExtern {
				rec.Target = &Target{
					Raw:      p.targetRaw,
					PkgPath:  p.pkgPath,
					Name:     p.name,
					RecvType: p.recvType,
					Resolved: []ResolvedLocation{},
				}
			}
			rec.Warnings = append(rec.Warnings, p.warnings...)
			if !hasUnsafe {
				rec.Warnings = append(rec.Warnings, WarnMissingUnsafeImport)
			}
			out = append(out, rec)
		}
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			emit(d.Doc, d.Name.Name, DeclFunc, d.Body != nil)
		case *ast.GenDecl:
			// Only var [ast.GenDecl]s can carry linkname directives; imports,
			// types, and consts cannot. Two source layouts are valid.
			//
			// Layout 1: bare var, directive above the keyword.
			// d.Doc carries the directive; [ast.ValueSpec].Doc is nil.
			//
			//   //go:linkname X pkg.X
			//   var X T
			//
			// Layout 2: var block, directive above one [ast.ValueSpec].
			// [ast.ValueSpec].Doc carries the directive; d.Doc may hold an
			// unrelated group docstring.
			//
			//   var (
			//       //go:linkname X pkg.X
			//       X T
			//   )
			//
			// Walk both levels and emit one Record per directive,
			// attributing each to the name it actually documents.
			//
			// `hasBody' for a var is whether the spec has its own
			// initializer expression. An initialized var owns its
			// storage and is the push side; a bodyless var declaration
			// is a slot the linker rebinds, so it is the pull side.
			// This mirrors the (form, has-body) -> direction table for
			// functions exactly.
			if d.Tok != token.VAR {
				continue
			}
			emit(d.Doc, genDeclFirstName(d), DeclVar, firstSpecHasInit(d))
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Doc == nil || len(vs.Names) == 0 {
					continue
				}
				emit(vs.Doc, vs.Names[0].Name, DeclVar, len(vs.Values) > 0)
			}
		}
	}
	return out
}

// directionFor classifies a directive as push or pull based on its form and
// whether the declaration has a body. See the Direction docstring in record.go
// for the full table.
//
// FormTwoArgExtern is always a pull: the target is a bare linker symbol
// supplied by cgo, the runtime, an assembly file, FIPS, or a sanitizer hook --
// never by a Go-side body.
func directionFor(form Form, hasBody bool) Direction {
	if form == FormTwoArgExtern {
		return DirectionPull
	}
	if hasBody {
		return DirectionPush
	}
	return DirectionPull
}

// fileImportsUnsafe reports whether the file imports "unsafe" in any form
// (blank, named, or default). The compiler requires the importing file to
// import "unsafe" for //go:linkname to be honored, but does not care
// whether the import is blank-aliased; the runtime sources use both
// `import _ "unsafe"` and plain `import "unsafe"` interchangeably alongside
// linkname directives.
func fileImportsUnsafe(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		path, err := strconv.Unquote(imp.Path.Value)
		if err == nil && path == "unsafe" {
			return true
		}
	}
	return false
}

// genDeclFirstName returns the first declared name in a var [ast.GenDecl], or
// the empty string if none can be determined. Used as the DeclName for
// directives attached to the [ast.GenDecl] itself (the bare-var layout).
func genDeclFirstName(d *ast.GenDecl) string {
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) == 0 {
			continue
		}
		return vs.Names[0].Name
	}
	return ""
}

// firstSpecHasInit reports whether the first [ast.ValueSpec] in a var [ast.GenDecl]
// has an initializer expression. Used as the var-equivalent of
// [ast.FuncDecl.Body] != nil for the bare-var layout, where the directive
// sits on the [ast.GenDecl] itself rather than on a specific [ast.ValueSpec].
//
//	//go:linkname X pkg.X       // Layout 1: directive on GenDecl.Doc
//	var X = expr                // -> firstSpecHasInit(d) == true (push)
//
//	//go:linkname X pkg.X
//	var X int                   // -> firstSpecHasInit(d) == false (pull)
//
// We attribute the directive to the first spec because that is the
// name [genDeclFirstName] returns; in practice these directives sit on
// single-spec [ast.GenDecl]s, so first-spec is the only spec.
func firstSpecHasInit(d *ast.GenDecl) bool {
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		return len(vs.Values) > 0
	}
	return false
}
