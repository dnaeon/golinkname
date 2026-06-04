// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

// scanFile parses a single Go source file and returns one Record per
// //go:linkname directive observed on a top-level FuncDecl or GenDecl.
//
// relPath is the module-relative, slash-separated path used as Record.File.
// absPath is the on-disk path passed to the parser.
//
// On parse failure, scanFile returns a single Record with ParseError set
// (no other fields populated besides File and SchemaVersion).
//
// The Record.Target.Resolved slice is left empty; resolution happens in
// resolve.go. The missing-unsafe-import warning is added here based on
// file-level state.
func scanFile(absPath, relPath string) []Record {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, absPath, nil,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return []Record{{
			SchemaVersion: SchemaVersion,
			File:          relPath,
			ParseError:    err.Error(),
		}}
	}

	hasUnsafe := fileImportsUnsafeBlank(file)

	var out []Record
	emit := func(doc *ast.CommentGroup, declName string, declKind DeclKind) {
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
				LocalName:       p.localName,
				DeclName:        declName,
				DeclKind:        declKind,
				HasUnsafeImport: hasUnsafe,
				Warnings:        []string{},
			}
			if p.form == FormTwoArg {
				rec.Target = &Target{
					Raw:      p.targetRaw,
					PkgPath:  p.pkgPath,
					Name:     p.name,
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
			emit(d.Doc, d.Name.Name, DeclFunc)
		case *ast.GenDecl:
			// Only var GenDecls can carry linkname directives; imports,
			// types, and consts cannot. Two source layouts are valid.
			//
			// Layout 1: bare var, directive above the keyword.
			// d.Doc carries the directive; ValueSpec.Doc is nil.
			//
			//   //go:linkname X pkg.X
			//   var X T
			//
			// Layout 2: var block, directive above one ValueSpec.
			// ValueSpec.Doc carries the directive; d.Doc may hold an
			// unrelated group docstring.
			//
			//   var (
			//       //go:linkname X pkg.X
			//       X T
			//   )
			//
			// Walk both levels and emit one Record per directive,
			// attributing each to the name it actually documents.
			if d.Tok != token.VAR {
				continue
			}
			emit(d.Doc, genDeclFirstName(d), DeclVar)
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Doc == nil || len(vs.Names) == 0 {
					continue
				}
				emit(vs.Doc, vs.Names[0].Name, DeclVar)
			}
		}
	}
	return out
}

// fileImportsUnsafeBlank reports whether the file contains an
// `import _ "unsafe"` statement. The compiler enables //go:linkname only in
// files with this exact import, so it is recorded for diagnostics.
func fileImportsUnsafeBlank(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "unsafe" {
			continue
		}
		if imp.Name != nil && imp.Name.Name == "_" {
			return true
		}
	}
	return false
}

// genDeclFirstName returns the first declared name in a var GenDecl, or
// the empty string if none can be determined. Used as the DeclName for
// directives attached to the GenDecl itself (the bare-var layout).
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
