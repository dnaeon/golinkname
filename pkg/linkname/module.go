// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// Module describes a Go module rooted on disk.
type Module struct {
	// Root is the absolute path to the directory containing go.mod.
	Root string

	// Path is the module path declared in go.mod (e.g. "example.com/m").
	Path string
}

// FindModule walks up from start until it finds a directory containing a
// go.mod, parses it, and returns the resulting Module. If start is empty
// the current working directory is used. Returns an error if no enclosing
// module is found or the go.mod cannot be parsed.
func FindModule(start string) (*Module, error) {
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, fmt.Errorf("absolute path: %w", err)
	}

	dir := abs
	for {
		gomod := filepath.Join(dir, "go.mod")
		if fi, err := os.Stat(gomod); err == nil && !fi.IsDir() {
			data, err := os.ReadFile(gomod)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", gomod, err)
			}
			modPath := modfile.ModulePath(data)
			if modPath == "" {
				return nil, fmt.Errorf("%s: missing module path", gomod)
			}
			return &Module{Root: dir, Path: modPath}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no go.mod found in %s or any parent", abs)
		}
		dir = parent
	}
}

// WalkGoFiles returns the relative paths (slash-separated, relative to
// m.Root) of every .go file under the module that should be considered
// for indexing.
//
// Skipped:
//   - directories named "vendor" or "testdata"
//   - directories starting with "." or "_" (Go's standard tool exclusions)
//   - subtrees containing their own go.mod (sub-modules)
//
// Both regular and _test.go files are included; build tags are NOT
// evaluated (every file is parsed regardless of GOOS/GOARCH).
func (m *Module) WalkGoFiles() ([]string, error) {
	var out []string
	rootClean := filepath.Clean(m.Root)

	err := filepath.WalkDir(rootClean, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == rootClean {
				return nil
			}
			name := d.Name()
			if name == "vendor" || name == "testdata" {
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return fs.SkipDir
			}
			// Skip nested modules.
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(rootClean, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
