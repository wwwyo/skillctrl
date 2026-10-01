// Package jsonfmt renders JSON with the byte-level conventions the lock files
// and CLI output have always used.
//
// Go's encoder escapes HTML characters and indents differently from the
// previous implementation, so both cases are disabled explicitly: a skill name
// or source URL containing "&" must stay readable, and the on-disk lock files
// must remain diff-stable for the versions that already consume them.
package jsonfmt

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Compact renders a single-line JSON document followed by a newline.
func Compact(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// File renders the two-space indented, newline-terminated form written to lock
// files so existing diffs stay minimal.
func File(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// WriteFile writes the indented form atomically enough for Git-tracked files by
// creating the parent directory and replacing the file in one write.
func WriteFile(path string, value any) error {
	data, err := File(value)
	if err != nil {
		return err
	}
	return writeBytes(path, data)
}

// Print writes the compact form to standard output.
func Print(value any) error {
	data, err := Compact(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Print(string(data)); err != nil {
		return err
	}
	return nil
}
