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
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/dnaeon/golinkname/pkg/linkname"
)

func main() {
	cmd := newCommand(os.Stdout, os.Stderr)
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newCommand builds the root *cli.Command. Stdout and stderr are passed
// in so tests can capture output without touching os.Std*.
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
		Name:                  "golinkname",
		Usage:                 "Index //go:linkname directives in a Go module.",
		Writer:                stdout,
		ErrWriter:             stderr,
		EnableShellCompletion: true,
		CommandNotFound: func(_ context.Context, cmd *cli.Command, name string) {
			fmt.Fprintf(cmd.ErrWriter, "golinkname: unknown subcommand %q\n", name)
		},
		Commands: []*cli.Command{
			{
				Name:      "index",
				Usage:     "Emit a JSON array of every directive in the module.",
				ArgsUsage: " ",
				Flags:     []cli.Flag{dirFlag, prettyFlag},
				Action: func(_ context.Context, cmd *cli.Command) error {
					return runIndex(cmd, cmd.String("dir"), cmd.Bool("pretty"))
				},
			},
			{
				Name:      "refs",
				Usage:     "Find directives whose target is exactly <pkgpath>.<name>.",
				ArgsUsage: "<pkgpath>.<name>",
				Flags:     []cli.Flag{dirFlag, prettyFlag},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.NArg() != 1 {
						return fmt.Errorf("golinkname refs: expected exactly one <pkgpath>.<name> argument")
					}
					return runRefs(cmd, cmd.String("dir"), cmd.Bool("pretty"), cmd.Args().First())
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

// urfave/cli does not propagate Writer to subcommands; each subcommand
// defaults to os.Stdout during setup. Actions reach for cmd.Root().Writer
// to use the writer that was configured on the root in newCommand.
func runIndex(cmd *cli.Command, dir string, pretty bool) error {
	records, err := linkname.Index(dir)
	if err != nil {
		return fmt.Errorf("golinkname: %w", err)
	}
	return emitJSON(cmd.Root().Writer, records, pretty)
}

func runRefs(cmd *cli.Command, dir string, pretty bool, query string) error {
	dot := strings.LastIndex(query, ".")
	if dot <= 0 || dot == len(query)-1 {
		return fmt.Errorf("golinkname refs: %q is not a valid <pkgpath>.<name>", query)
	}
	wantPkg, wantName := query[:dot], query[dot+1:]

	records, err := linkname.Index(dir)
	if err != nil {
		return fmt.Errorf("golinkname: %w", err)
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

func runList(cmd *cli.Command, dir, file string) error {
	records, err := linkname.Index(dir)
	if err != nil {
		return fmt.Errorf("golinkname: %w", err)
	}

	tw := tabwriter.NewWriter(cmd.Root().Writer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE:LINE\tFORM\tLOCAL\tTARGET\tRESOLVED\tWARNINGS")
	for _, r := range records {
		if file != "" && r.File != file {
			continue
		}
		if r.ParseError != "" {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\tparse-error: %s\n", r.File, r.ParseError)
			continue
		}
		target := "-"
		resolved := "-"
		if r.Target != nil {
			target = r.Target.Raw
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
		warnings := "-"
		if len(r.Warnings) > 0 {
			warnings = strings.Join(r.Warnings, ",")
		}
		fmt.Fprintf(tw, "%s:%d\t%s\t%s\t%s\t%s\t%s\n",
			r.File, r.Line, r.Form, r.LocalName, target, resolved, warnings)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("golinkname: %w", err)
	}
	return nil
}

func emitJSON(w io.Writer, records []linkname.Record, pretty bool) error {
	// Always emit a JSON array, even when empty.
	if records == nil {
		records = []linkname.Record{}
	}
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	enc.SetEscapeHTML(false)
	if err := enc.Encode(records); err != nil {
		return fmt.Errorf("golinkname: %w", err)
	}
	return nil
}
