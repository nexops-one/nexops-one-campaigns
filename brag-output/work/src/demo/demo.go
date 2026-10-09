// SPDX-License-Identifier: Apache-2.0

// Package demo holds the demo's sample register and evidence, embedded in
// the binary, and the bootstrap that prepares a sample workspace. The
// sample organization is fictitious; nothing here is real data.
package demo

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

//go:generate go run ./gen

//go:embed sample-register.xlsx evidence
var files embed.FS

// RegisterFile is the file name of the sample register.
const RegisterFile = "sample-register.xlsx"

// Register returns the sample register workbook.
func Register() []byte {
	data, err := files.ReadFile(RegisterFile)
	if err != nil {
		panic(err)
	}
	return data
}

// Evidence returns the sample evidence documents and their SHA256SUMS.
func Evidence() fs.FS {
	sub, err := fs.Sub(files, "evidence")
	if err != nil {
		panic(err)
	}
	return sub
}

// Sums renders the sha256sum-style checksum list of the documents in fsys
// (every file except SHA256SUMS), sorted by name.
func Sums(fsys fs.FS) ([]byte, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "SHA256SUMS" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var buf bytes.Buffer
	for _, n := range names {
		data, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&buf, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return buf.Bytes(), nil
}

// WriteEvidence copies the sample evidence documents into dir, which the
// engine can then use as its evidence root.
func WriteEvidence(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(Evidence(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(Evidence(), p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, p), data, 0o644)
	})
}
