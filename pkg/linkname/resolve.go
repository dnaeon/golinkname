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

// resolver finds source locations for a (pkgPath, name) pair within the scope
// of a single Module. The current scope is intentionally narrow: only targets
// that map to a directory inside the module's own tree are resolved. Stdlib and
// external-dependency targets are left unresolved (resolve returns an empty
// slice without error).
//
// Each resolver instance holds a per-directory parse cache: the first resolve()
// call against a target directory parses every .go file in it, and subsequent
// calls (different name, or different recvType) reuse those ASTs. This is
// critical for large modules like the stdlib where a single package
// (e.g. runtime, reflect) is the target of dozens of directives -- without the
// cache we re-parse the package's source every time, which dominated CPU on the
// stdlib walk.
type resolver struct {
	module *Module
	fset   *token.FileSet
	cache  map[string]*pkgCache
}

// pkgCache holds parsed ASTs for a single directory, grouped by the `package`
// declaration of each file. A directory may legitimately contain files from two
// packages -- a `foo` package and its `foo_test' black-box test variant -- and
// a linkname target with pkgPath="foo" must only resolve against `package foo'
// files. Without this grouping, a bodyless test-file decl that is itself the
// *pull side* of a linkname directive would falsely satisfy the
// resolution. `missing' records read or parse failure: we cache the negative
// result so a repeated lookup does not retry the same broken file. Once cached,
// entries are immutable.
type pkgCache struct {
	byPackage map[string][]cachedFile
	missing   bool
}

type cachedFile struct {
	abs  string
	rel  string
	file *ast.File
}

func newResolver(m *Module) *resolver {
	return &resolver{
		module: m,
		fset:   token.NewFileSet(),
		cache:  make(map[string]*pkgCache),
	}
}

// resolve returns every top-level declaration of name inside pkgPath, when
// pkgPath maps to a directory in the module. Multiple results are possible when
// build-tag-gated source files declare the same name; we record all of them
// since we do not evaluate build tags.
//
// recvType, when non-empty, narrows the search to methods whose receiver type
// identifier matches it. The leading "*" on a pointer receiver is
// ignored. recvType empty means "free functions and top-level vars only" -- the
// original behavior.
//
// Only files whose `package' declaration matches the last segment of pkgPath
// are searched -- the `_test'-suffixed black-box test variant that may live in
// the same directory is excluded. Go's package layout rules guarantee the last
// import-path segment equals the package name on disk (the only legal
// divergence is the `_test' suffix).
//
// An empty result with a nil error means "could not be resolved within the
// configured scope" -- not an error condition.
func (r *resolver) resolve(pkgPath, name, recvType string) []ResolvedLocation {
	if pkgPath == "" || name == "" {
		return nil
	}
	dir, ok := r.packageDir(pkgPath)
	if !ok {
		return nil
	}
	pc := r.loadPkg(dir)
	if pc.missing {
		return nil
	}

	files := pc.byPackage[lastPathSegment(pkgPath)]
	var out []ResolvedLocation
	for _, cf := range files {
		for _, decl := range cf.file.Decls {
			pos := matchTopLevelDecl(decl, name, recvType)
			if !pos.IsValid() {
				continue
			}
			p := r.fset.Position(pos)
			out = append(out, ResolvedLocation{
				File:     cf.rel,
				Line:     p.Line,
				Col:      p.Column,
				InModule: true,
			})
		}
	}
	return out
}

// lastPathSegment returns the substring after the last '/' in pkgPath, or
// pkgPath itself when there is no '/'. Used to recover the on-disk package name
// from a Go import path: `internal/synctest' -> `synctest', `time' ->
// `time'. Go's package layout rules require the last segment of an import path
// to equal the package's `package' declaration.
func lastPathSegment(pkgPath string) string {
	if i := strings.LastIndexByte(pkgPath, '/'); i >= 0 {
		return pkgPath[i+1:]
	}
	return pkgPath
}

// loadPkg returns the parse cache for dir, populating it on first use.  Files
// are grouped by their `package' declaration so resolve() can pick the right
// group for the target's import path. On any read or per-file parse error the
// entry is still returned (the successfully-parsed files are usable); only a
// directory-level read failure marks the entry missing.
func (r *resolver) loadPkg(dir string) *pkgCache {
	if pc, ok := r.cache[dir]; ok {
		return pc
	}
	pc := &pkgCache{byPackage: make(map[string][]cachedFile)}
	r.cache[dir] = pc

	entries, err := os.ReadDir(dir)
	if err != nil {
		pc.missing = true
		return pc
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		abs := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(r.fset, abs, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(r.module.Root, abs)
		if err != nil {
			continue
		}
		pkgName := file.Name.Name
		pc.byPackage[pkgName] = append(pc.byPackage[pkgName], cachedFile{
			abs:  abs,
			rel:  filepath.ToSlash(rel),
			file: file,
		})
	}
	return pc
}

// packageDir maps a Go import path to the directory holding its source inside
// the module. Returns ("", false) if pkgPath is not in-module.
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
	case r.module.IsStdlib:
		// Stdlib import paths are unprefixed (e.g. "runtime",
		// "crypto/internal/fips140"). Map them directly under the
		// module root.
		dir := filepath.Join(r.module.Root, filepath.FromSlash(pkgPath))
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, true
		}
		return "", false
	}
	return "", false
}

// matchTopLevelDecl returns the position of a top-level declaration named name,
// if decl declares it. For GenDecl the position returned is the position of the
// matching name inside its ValueSpec.
//
// When recvType is non-empty, only methods whose receiver type identifier
// equals recvType are matched (free functions and vars are skipped). The
// receiver may be a value or pointer receiver; the leading "*" is ignored. When
// recvType is empty the original behavior applies: methods are skipped (no
// unqualified linkname target can name a method without the explicit
// `(Recv).Method' syntax that produces a non-empty recvType).
func matchTopLevelDecl(decl ast.Decl, name, recvType string) token.Pos {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if recvType != "" {
			if d.Recv == nil || d.Name.Name != name {
				return token.NoPos
			}
			if recvIdent(d.Recv) == recvType {
				return d.Name.NamePos
			}
			return token.NoPos
		}
		// Skip methods: linkname targets without an explicit receiver
		// shape never name a method.
		if d.Recv != nil {
			return token.NoPos
		}
		if d.Name.Name == name {
			return d.Name.NamePos
		}
	case *ast.GenDecl:
		if recvType != "" {
			// Method targets never resolve to a var.
			return token.NoPos
		}
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

// recvIdent returns the bare type identifier of a method receiver list,
// stripping a leading "*" for pointer receivers. Returns "" if the receiver
// shape is anything other than a single (value or pointer) identifier --
// e.g. generic instantiations like `(*T[U])', which we do not navigate.
func recvIdent(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) != 1 {
		return ""
	}
	switch t := recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}
