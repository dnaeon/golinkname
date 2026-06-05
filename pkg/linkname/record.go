// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import "encoding/json"

// SchemaVersion is the version of the JSON output format. Bump when
// consumers must change to keep working.
const SchemaVersion = 1

// Form is the syntactic form of a //go:linkname directive.
type Form string

const (
	// FormOneArg is the one-argument form: //go:linkname localname.
	// Used on the origin side to mark a symbol as linkable.
	FormOneArg Form = "one-arg"

	// FormTwoArg is the two-argument form: //go:linkname localname pkgpath.name.
	// Used on the consuming side to alias a foreign symbol.
	FormTwoArg Form = "two-arg"

	// FormTwoArgExtern is the two-argument form whose second argument
	// is a bare linker symbol with no `pkgpath.name' shape:
	//   //go:linkname localname symname
	// The symbol is not a Go target -- it is supplied by cgo, the
	// runtime, a sanitizer (TSAN/libfuzzer), or a special compiler
	// identifier (e.g. `go:fipsinfo'). Well-formed, just not navigable
	// to a Go declaration; resolvers leave `Target.Resolved' empty.
	FormTwoArgExtern Form = "two-arg-extern"
)

// DeclKind classifies the declaration the directive sits on.
type DeclKind string

const (
	// DeclFunc indicates the directive sits on a function declaration.
	DeclFunc DeclKind = "func"

	// DeclVar indicates the directive sits on a top-level var declaration.
	DeclVar DeclKind = "var"
)

// Warning codes attached to records. Always serialized as a possibly-empty
// list so JSON consumers do not need null checks.
const (
	// WarnMissingUnsafeImport is emitted when a directive's file does
	// not import "unsafe" (a Go compiler requirement). Both `import "unsafe"`
	// and `import _ "unsafe"` satisfy the requirement.
	WarnMissingUnsafeImport = "missing-unsafe-import"

	// WarnMalformedDirective is emitted when a directive cannot be
	// parsed (wrong arg count, missing dot in target, empty pkgPath).
	WarnMalformedDirective = "malformed-directive"
)

// Record is one //go:linkname directive observation.
//
// A Record is also used to surface a per-file parse error: when ParseError
// is non-empty the rest of the fields except File are zero, and consumers
// must discriminate on (ParseError != "") vs (Form != "").
type Record struct {
	// SchemaVersion is the JSON contract version this Record adheres to.
	// Always equal to the package-level SchemaVersion constant.
	SchemaVersion int `json:"schemaVersion"`

	// File is the module-relative, slash-separated path of the source
	// file the directive was observed in.
	File string `json:"file"`

	// ParseError, when non-empty, marks this Record as a per-file parse
	// failure. Every other field except File is zero in that case.
	ParseError string `json:"parseError,omitempty"`

	// Line is the 1-indexed line number of the //go:linkname comment.
	Line int `json:"line,omitempty"`

	// Col is the 1-indexed column of the //go:linkname comment.
	Col int `json:"col,omitempty"`

	// Form is the syntactic form of the directive (one-arg or two-arg).
	Form Form `json:"form,omitempty"`

	// LocalName is the first argument of the directive -- the symbol in
	// the current package the directive applies to.
	LocalName string `json:"localName,omitempty"`

	// DeclName is the name of the Go declaration the directive sits on.
	// Usually equal to LocalName, but may differ when multiple directives
	// share one declaration's doc comment.
	DeclName string `json:"declName,omitempty"`

	// DeclKind classifies the declaration the directive sits on (func or var).
	DeclKind DeclKind `json:"declKind,omitempty"`

	// Target is the parsed second argument of a two-arg directive. Nil
	// for FormOneArg directives.
	Target *Target `json:"target"`

	// HasUnsafeImport reports whether the file containing the directive
	// imports "unsafe" (in any form -- blank, named, or default). The Go
	// compiler requires this import for //go:linkname to be honored.
	HasUnsafeImport bool `json:"hasUnsafeImport"`

	// Warnings is a list of warning codes attached to this Record.
	// Always non-nil; serialized as an empty array when clean so JSON
	// consumers do not need null checks.
	Warnings []string `json:"warnings"`
}

// MarshalJSON emits the compact `{schemaVersion, file, parseError}` shape
// for parse-error records, and the full struct otherwise. The compact
// shape is part of the schema-v1 contract: consumers discriminate on
// (parseError != "") without having to ignore zeroed fields.
func (r Record) MarshalJSON() ([]byte, error) {
	if r.ParseError != "" {
		return json.Marshal(struct {
			SchemaVersion int    `json:"schemaVersion"`
			File          string `json:"file"`
			ParseError    string `json:"parseError"`
		}{r.SchemaVersion, r.File, r.ParseError})
	}
	type recordJSON Record
	return json.Marshal(recordJSON(r))
}

// Target is the second argument of a two-argument //go:linkname directive.
type Target struct {
	// Raw is the verbatim second argument as it appears in the directive.
	Raw string `json:"raw"`

	// PkgPath is the import path portion of the target (everything before
	// the last dot in Raw). Empty for malformed directives.
	PkgPath string `json:"pkgPath"`

	// Name is the symbol name portion of the target (everything after the
	// last dot in Raw). Empty for malformed directives.
	Name string `json:"name"`

	// RecvType, when non-empty, marks the target as a method on the named
	// receiver type. Set for the `pkg.(Recv).Method' and
	// `pkg.(*Recv).Method' target shapes; the leading "*" is
	// stripped. Empty for free-function targets. Used by the resolver to
	// disambiguate methods that share an unqualified name across multiple
	// types in the same package.
	RecvType string `json:"recvType,omitempty"`

	// Resolved is the list of source locations matching PkgPath.Name.
	// Always non-nil; an empty slice means the target was not found
	// (e.g. stdlib pulls, or unresolved out-of-module symbols).
	Resolved []ResolvedLocation `json:"resolved"`
}

// ResolvedLocation is one source location matching a Target's pkgPath.name.
// A single target may resolve to multiple locations when the package has
// build-tag-gated variants of the same top-level decl.
type ResolvedLocation struct {
	// File is the module-relative, slash-separated path of the source
	// file containing the matching declaration.
	File string `json:"file"`

	// Line is the 1-indexed line number of the matching declaration's
	// identifier.
	Line int `json:"line"`

	// Col is the 1-indexed column of the matching declaration's identifier.
	Col int `json:"col"`

	// InModule reports whether the matching file lives inside the
	// indexed module.
	InModule bool `json:"inModule"`
}
