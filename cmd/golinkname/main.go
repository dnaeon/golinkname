// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

// Command golinkname indexes //go:linkname compiler directives in a Go
// module and emits a stable JSON contract suitable for editor and LSP
// integration.
//
// Usage:
//
//	golinkname index [--dir DIR] [--pretty]
//	golinkname refs  [--dir DIR] [--pretty] <pkgpath>.<name>
//	golinkname list  [--dir DIR] [--file PATH]
//
// Exit codes: 0 success, 1 any error.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/dnaeon/golinkname/pkg/linkname"
)

// progName is the name of the CLI tool.
const progName = "golinkname"

func main() {
	cmd := newCommand(os.Stdout, os.Stderr)
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		os.Exit(1)
	}
}

// newCommand builds the root [*cli.Command]. Stdout and stderr are passed in so
// tests can capture output without touching os.Std*.
func newCommand(stdout, stderr io.Writer) *cli.Command {
	dirFlag := &cli.StringFlag{
		Name:    "dir",
		Aliases: []string{"C"},
		Usage:   "module root (defaults to current directory)",
	}
	prettyFlag := &cli.BoolFlag{
		Name:  "pretty",
		Usage: "indent JSON output",
	}

	return &cli.Command{
		Name:                  progName,
		Usage:                 "Index //go:linkname directives in a Go module.",
		Writer:                stdout,
		ErrWriter:             stderr,
		EnableShellCompletion: true,
		CommandNotFound: func(_ context.Context, cmd *cli.Command, name string) {
			fmt.Fprintf(cmd.ErrWriter, "%s: unknown subcommand %q\n", progName, name)
		},
		Commands: []*cli.Command{
			{
				Name:      "index",
				Usage:     "Emit a JSON array of every directive in the module.",
				ArgsUsage: " ",
				Flags: []cli.Flag{
					dirFlag,
					prettyFlag,
					&cli.StringFlag{
						Name:  "file",
						Usage: "restrict output to this module-relative file",
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					return runIndex(cmd, cmd.String("dir"), cmd.Bool("pretty"), cmd.String("file"))
				},
			},
			{
				Name:      "refs",
				Usage:     "Find directives whose target is exactly <pkgpath>.<name>.",
				ArgsUsage: "<pkgpath>.<name>",
				Flags:     []cli.Flag{dirFlag, prettyFlag},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 {
						return fmt.Errorf("refs: expected exactly one <pkgpath>.<name> argument")
					}
					return runRefs(cmd, cmd.String("dir"), cmd.Bool("pretty"), cmd.Args().First())
				},
			},
			{
				Name:      "related",
				Usage:     "Find every directive related to <pkgpath>.<name> (target side or local side).",
				ArgsUsage: "<pkgpath>.<name>",
				Flags:     []cli.Flag{dirFlag, prettyFlag},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 {
						return fmt.Errorf("related: expected exactly one <pkgpath>.<name> argument")
					}
					return runRelated(cmd, cmd.String("dir"), cmd.Bool("pretty"), cmd.Args().First())
				},
			},
			{
				Name:      "list",
				Usage:     "Human-readable table of directives.",
				ArgsUsage: " ",
				Flags: []cli.Flag{
					dirFlag,
					&cli.StringFlag{
						Name:  "file",
						Usage: "restrict output to this module-relative file",
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					return runList(cmd, cmd.String("dir"), cmd.String("file"))
				},
			},
		},
	}
}

// runIndex executes the `index' subcommand: walk the module rooted at dir, emit
// a JSON array of every directive observed, and (when file is non-empty) drop
// records whose File field does not match. The filter is applied post-walk --
// the full module is still indexed -- so that the on-disk JSON shape is
// identical to a full `index' run, just narrower.
//
// urfave/cli does not propagate Writer to subcommands; each subcommand defaults
// to [os.Stdout] during setup. Actions reach for cmd.Root().Writer to use the
// writer that was configured on the root in [newCommand].
func runIndex(cmd *cli.Command, dir string, pretty bool, file string) error {
	records, err := linkname.Index(dir)
	if err != nil {
		return err
	}
	if file != "" {
		filtered := make([]linkname.Record, 0, len(records))
		for _, r := range records {
			if r.File == file {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}
	return emitJSON(cmd.Root().Writer, records, pretty)
}

// runRefs executes the `refs' subcommand: emit a JSON array of every directive
// in the module whose target's `<pkgPath>.<Name>' equals the query.  One-arg
// directives (no target) are skipped. For symmetric lookup that also matches on
// the directive's local-side qualified name, see [runRelated].
func runRefs(cmd *cli.Command, dir string, pretty bool, query string) error {
	wantPkg, wantName, err := splitQuery(query, "refs")
	if err != nil {
		return err
	}

	records, err := linkname.Index(dir)
	if err != nil {
		return err
	}
	filtered := make([]linkname.Record, 0, len(records))
	for _, r := range records {
		if r.Target == nil {
			continue
		}
		if r.Target.PkgPath == wantPkg && r.Target.Name == wantName {
			filtered = append(filtered, r)
		}
	}
	return emitJSON(cmd.Root().Writer, filtered, pretty)
}

// runRelated returns every directive related to <pkgpath>.<name>: both
// directives whose target matches the query (the same set [runRefs] returns)
// and directives whose own local-side qualified name matches. The local- side
// match makes this command symmetric -- standing on either end of a push/pull
// bridge surfaces both ends, plus any other directive aimed at the same symbol.
//
// The local-side qualified name is `<filePkgPath>.LocalName`, where
// [filePkgPath] is computed from the directive's file path and the indexed
// module's path (with the stdlib prefix-elision rule applied: paths inside a
// `module std' tree are unprefixed).
func runRelated(cmd *cli.Command, dir string, pretty bool, query string) error {
	wantPkg, wantName, err := splitQuery(query, "related")
	if err != nil {
		return err
	}

	mod, err := linkname.FindModule(dir)
	if err != nil {
		return err
	}
	records, err := linkname.Index(dir)
	if err != nil {
		return err
	}

	filtered := make([]linkname.Record, 0, len(records))
	for _, r := range records {
		if r.ParseError != "" {
			continue
		}
		// Target side: directive's target is <wantPkg>.<wantName>.
		if r.Target != nil && r.Target.PkgPath == wantPkg && r.Target.Name == wantName {
			filtered = append(filtered, r)
			continue
		}
		// Local side: directive sits on a decl whose own qualified name
		// is <wantPkg>.<wantName>. LocalName, not DeclName -- a single
		// decl may carry multiple linkname directives with different
		// LocalNames (rare but legal), and the directive's identity is
		// its LocalName.
		if r.LocalName == wantName && filePkgPath(mod, r.File) == wantPkg {
			filtered = append(filtered, r)
		}
	}
	return emitJSON(cmd.Root().Writer, filtered, pretty)
}

// splitQuery validates a `<pkgpath>.<name>' query string and returns its two
// halves. The error message is prefixed with cmd so users see `refs:' or
// `related:' rather than a generic message; [main] adds the `golinkname:'
// program prefix on top.
func splitQuery(query, cmd string) (string, string, error) {
	dot := strings.LastIndex(query, ".")
	if dot <= 0 || dot == len(query)-1 {
		return "", "", fmt.Errorf("%s: %q is not a valid <pkgpath>.<name>", cmd, query)
	}
	return query[:dot], query[dot+1:], nil
}

// filePkgPath returns the import path of the package containing a
// module-relative file. For a regular module, the result is
// `<module-path>/<dir>'; for a stdlib checkout, the module prefix is elided (so
// `runtime/proc.go' -> `runtime', not `std/runtime'), matching the resolver's
// stdlib special case.
//
// Files at the module root are treated specially: a regular module returns its
// module path; a stdlib checkout returns "builtin", which is the canonical
// import path of files at $GOROOT/src.
func filePkgPath(mod *linkname.Module, relFile string) string {
	dir := path.Dir(relFile)
	if mod.IsStdlib {
		if dir == "." {
			return "builtin"
		}
		return dir
	}
	if dir == "." {
		return mod.Path
	}
	return mod.Path + "/" + dir
}

// runList executes the `list' subcommand: walk the module and render every
// directive as a tab-aligned table to stdout. Parse-error records are shown
// inline. When file is non-empty the table is restricted to that
// module-relative path. This is the only non-JSON output path; everything else
// emits JSON for downstream consumers.
func runList(cmd *cli.Command, dir, file string) error {
	records, err := linkname.Index(dir)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(cmd.Root().Writer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE:LINE\tFORM\tDIR\tLOCAL\tTARGET\tRESOLVED\tWARNINGS")
	for _, r := range records {
		if file != "" && r.File != file {
			continue
		}
		if r.ParseError != "" {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\tparse-error: %s\n", r.File, r.ParseError)
			continue
		}
		target := "-"
		resolved := "-"
		if r.Target != nil {
			target = r.Target.Raw
			// two-arg-extern targets are bare linker symbols (cgo,
			// asm, FIPS, sanitizer hooks) -- not Go-side decls. Show
			// "-" rather than a misleading "0", which implies "we
			// looked and found none".
			if r.Form == linkname.FormTwoArgExtern {
				resolved = "-"
			} else {
				switch len(r.Target.Resolved) {
				case 0:
					resolved = "0"
				case 1:
					loc := r.Target.Resolved[0]
					resolved = fmt.Sprintf("%s:%d", loc.File, loc.Line)
				default:
					resolved = fmt.Sprintf("%d locations", len(r.Target.Resolved))
				}
			}
		}
		direction := string(r.Direction)
		if direction == "" {
			direction = "-"
		}
		warnings := "-"
		if len(r.Warnings) > 0 {
			warnings = strings.Join(r.Warnings, ",")
		}
		fmt.Fprintf(tw, "%s:%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.File, r.Line, formatKindForm(r.DeclKind, r.Form), direction, r.LocalName, target, resolved, warnings)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return nil
}

// formatKindForm renders the FORM column as `kind/form' (e.g.  `func/two-arg',
// `var/two-arg-extern'). The combined column avoids adding a separate KIND
// column to an already wide table while still surfacing the declaration kind,
// which matters for direction interpretation: a `var/one-arg' push has
// different mechanics from a `func/one-arg' push (initializer vs body).
//
// When kind or form is missing (parse-error records, malformed directives) the
// corresponding side renders as "-".
func formatKindForm(kind linkname.DeclKind, form linkname.Form) string {
	k := string(kind)
	if k == "" {
		k = "-"
	}
	f := string(form)
	if f == "" {
		f = "-"
	}
	return k + "/" + f
}

// emitJSON encodes records to w as a JSON array, indented when pretty is
// true. A nil records slice is normalized to an empty array so consumers always
// receive `[]' rather than `null'. HTML escaping is disabled so import paths
// and target strings round-trip verbatim.
func emitJSON(w io.Writer, records []linkname.Record, pretty bool) error {
	if records == nil {
		records = []linkname.Record{}
	}
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	enc.SetEscapeHTML(false)
	if err := enc.Encode(records); err != nil {
		return err
	}
	return nil
}
