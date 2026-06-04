// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

// Package linkname provides a workspace-wide index of Go //go:linkname
// compiler directives. It walks a Go module, parses every linkname
// directive (both one-argument and two-argument forms), source-resolves
// targets that map to in-module packages, and returns a stable JSON-shaped
// Record stream suitable for editor and LSP integration.
//
// The package intentionally avoids go/types and go/packages: it operates
// entirely on go/parser output, which is sufficient because //go:linkname
// targets are syntactic by definition.
package linkname

import "path/filepath"

// Index walks the Go module enclosing dir and returns one Record per
// //go:linkname directive observed, plus one parseError record per file
// the parser could not handle. Records are returned in walk order
// (filesystem traversal order), with directives within a file in source
// order.
//
// dir may be empty, in which case the current working directory is used.
// If no enclosing go.mod is found, Index returns an error.
func Index(dir string) ([]Record, error) {
	mod, err := FindModule(dir)
	if err != nil {
		return nil, err
	}
	files, err := mod.WalkGoFiles()
	if err != nil {
		return nil, err
	}

	r := newResolver(mod)
	var out []Record
	for _, rel := range files {
		abs := filepath.Join(mod.Root, filepath.FromSlash(rel))
		recs := scanFile(abs, rel)
		for i := range recs {
			if recs[i].Target != nil {
				recs[i].Target.Resolved = r.Resolve(recs[i].Target.PkgPath, recs[i].Target.Name)
				if recs[i].Target.Resolved == nil {
					recs[i].Target.Resolved = []ResolvedLocation{}
				}
			}
		}
		out = append(out, recs...)
	}
	return out, nil
}
