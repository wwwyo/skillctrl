package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveIsRepeatableAndInstallable(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, data := range map[string]string{"LICENSE": "MIT fixture\n", "THIRD_PARTY_NOTICES": "BSD fixture\n", "binary": "binary fixture\n"} {
		if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"first.tar.gz", "second.tar.gz"} {
		if err := archive(name, "binary"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := os.ReadFile("first.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile("second.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("unchanged release inputs produced different archives")
	}
	compressed, err := gzip.NewReader(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	tape := tar.NewReader(compressed)
	for _, want := range []struct {
		name, data string
		mode       int64
	}{{"LICENSE", "MIT fixture\n", 0o644}, {"THIRD_PARTY_NOTICES", "BSD fixture\n", 0o644}, {"skillctrl", "binary fixture\n", 0o755}} {
		header, err := tape.Next()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tape)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != want.name || header.Mode != want.mode || string(data) != want.data || header.ModTime.Unix() != 0 {
			t.Fatalf("unexpected archive entry: %+v", header)
		}
		if filepath.Base(header.Name) != header.Name {
			t.Fatalf("archive entry escapes the installation directory: %q", header.Name)
		}
	}
	if _, err := tape.Next(); err != io.EOF {
		t.Fatalf("unexpected trailing archive entry: %v", err)
	}
}
