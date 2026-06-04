// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// resolver finds source locations for a (pkgPath, name) pair within the
// scope of a single Module. The current scope is intentionally narrow: only
// targets that map to a directory inside the module's own tree are
// resolved. Stdlib and external-dependency targets are left unresolved
// (Resolve returns an empty slice without error).
type resolver struct {
	module *Module
}

func newResolver(m *Module) *resolver {
	return &resolver{module: m}
}

// Resolve returns every top-level declaration of name inside pkgPath, when
// pkgPath maps to a directory in the module. Multiple results are possible
// when build-tag-gated source files declare the same name; we record all
// of them since we do not evaluate build tags.
//
// An empty result with a nil error means "could not be resolved within the
// configured scope" -- not an error condition.
func (r *resolver) Resolve(pkgPath, name string) []ResolvedLocation {
	if pkgPath == "" || name == "" {
		return nil
	}
	dir, ok := r.packageDir(pkgPath)
	if !ok {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	fset := token.NewFileSet()
	var out []ResolvedLocation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		abs := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			pos := matchTopLevelDecl(decl, name)
			if !pos.IsValid() {
				continue
			}
			rel, err := filepath.Rel(r.module.Root, abs)
			if err != nil {
				continue
			}
			p := fset.Position(pos)
			out = append(out, ResolvedLocation{
				File:     filepath.ToSlash(rel),
				Line:     p.Line,
				Col:      p.Column,
				InModule: true,
			})
		}
	}
	return out
}

// packageDir maps a Go import path to the directory holding its source
// inside the module. Returns ("", false) if pkgPath is not in-module.
func (r *resolver) packageDir(pkgPath string) (string, bool) {
	mod := r.module.Path
	switch {
	case pkgPath == mod:
		return r.module.Root, true
	case strings.HasPrefix(pkgPath, mod+"/"):
		sub := strings.TrimPrefix(pkgPath, mod+"/")
		dir := filepath.Join(r.module.Root, filepath.FromSlash(sub))
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, true
		}
		return "", false
	}
	return "", false
}

// matchTopLevelDecl returns the position of a top-level declaration named
// name, if decl declares it. For GenDecl the position returned is the
// position of the matching name inside its ValueSpec.
func matchTopLevelDecl(decl ast.Decl, name string) token.Pos {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		// Skip methods: linkname targets are unqualified, so a method
		// receiver would never match.
		if d.Recv != nil {
			return token.NoPos
		}
		if d.Name.Name == name {
			return d.Name.NamePos
		}
	case *ast.GenDecl:
		if d.Tok != token.VAR {
			return token.NoPos
		}
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, ident := range vs.Names {
				if ident.Name == name {
					return ident.NamePos
				}
			}
		}
	}
	return token.NoPos
}
