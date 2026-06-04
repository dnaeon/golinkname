// Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
// Use of this source code is governed by the BSD-3-Clause license that can
// be found in the LICENSE file.

package linkname

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

// wantFileName is the name of the golden file inside each txtar fixture
// that holds the expected JSON output.
const wantFileName = "want.json"

var update = flag.Bool("update", false, "regenerate testdata want.json goldens")

func TestIndex_Txtar(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txtar") {
			continue
		}
		t.Run(strings.TrimSuffix(e.Name(), ".txtar"), func(t *testing.T) {
			runTxtarFixture(t, filepath.Join("testdata", e.Name()))
		})
	}
}

func runTxtarFixture(t *testing.T, fixturePath string) {
	t.Helper()
	archive, err := txtar.ParseFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	var wantRaw []byte
	for _, f := range archive.Files {
		if f.Name == wantFileName {
			wantRaw = f.Data
			continue
		}
		dst := filepath.Join(tmp, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Index(tmp)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Sort by (file, line) so tests are deterministic regardless of
	// filesystem walk order.
	sort.SliceStable(got, func(i, j int) bool {
		if got[i].File != got[j].File {
			return got[i].File < got[j].File
		}
		return got[i].Line < got[j].Line
	})

	gotJSON, err := marshalRecords(got)
	if err != nil {
		t.Fatal(err)
	}

	if *update {
		// Replace the want.json content in the archive.
		updated := append([]txtar.File(nil), archive.Files...)
		found := false
		for i := range updated {
			if updated[i].Name == wantFileName {
				updated[i].Data = append(gotJSON, '\n')
				found = true
				break
			}
		}
		if !found {
			updated = append(updated, txtar.File{Name: wantFileName, Data: append(gotJSON, '\n')})
		}
		archive.Files = updated
		if err := os.WriteFile(fixturePath, txtar.Format(archive), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	if wantRaw == nil {
		t.Fatalf("fixture %s has no %s (run with -update to generate)", fixturePath, wantFileName)
	}

	// Normalize parseError comparisons: any non-empty parseError matches
	// the sentinel "PRESENT" in the golden, since the exact wording is
	// dependent on the Go parser version.
	gotNorm := normalizeForCompare(gotJSON)
	wantNorm := normalizeForCompare(wantRaw)

	if !bytes.Equal(gotNorm, wantNorm) {
		t.Errorf("Index output mismatch.\n--- want ---\n%s\n--- got ---\n%s\n", wantNorm, gotNorm)
	}
}

// marshalRecords renders records as indented JSON. Empty Resolved slices
// are preserved as []. Records with parseError are emitted compactly
// (no zero fields).
func marshalRecords(records []Record) ([]byte, error) {
	// Use a per-record encoder so parseError records don't carry the rest
	// of Record's zero-value baggage in the output.
	type compactErrRecord struct {
		SchemaVersion int    `json:"schemaVersion"`
		File          string `json:"file"`
		ParseError    string `json:"parseError"`
	}
	var pieces []json.RawMessage
	for _, r := range records {
		var raw []byte
		var err error
		if r.ParseError != "" {
			raw, err = json.MarshalIndent(compactErrRecord{
				SchemaVersion: r.SchemaVersion,
				File:          r.File,
				ParseError:    r.ParseError,
			}, "  ", "  ")
		} else {
			raw, err = json.MarshalIndent(r, "  ", "  ")
		}
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, raw)
	}
	var buf bytes.Buffer
	buf.WriteString("[")
	for i, p := range pieces {
		if i == 0 {
			buf.WriteString("\n  ")
		} else {
			buf.WriteString(",\n  ")
		}
		buf.Write(p)
	}
	if len(pieces) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("]")
	return buf.Bytes(), nil
}

// normalizeForCompare canonicalizes a JSON byte slice for comparison:
// unmarshals and re-marshals so whitespace is consistent, and replaces
// any non-empty parseError with "PRESENT".
func normalizeForCompare(in []byte) []byte {
	var arr []map[string]any
	if err := json.Unmarshal(in, &arr); err != nil {
		// Fall back to raw bytes if the input isn't valid JSON; the
		// caller will surface the diff.
		return in
	}
	for _, obj := range arr {
		if pe, ok := obj["parseError"].(string); ok && pe != "" {
			obj["parseError"] = "PRESENT"
		}
	}
	out, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return in
	}
	return out
}
