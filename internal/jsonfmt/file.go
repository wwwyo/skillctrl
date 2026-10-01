package jsonfmt

import (
	"os"
	"path/filepath"
)

func writeBytes(path string, data []byte) error {
	if directory := filepath.Dir(path); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}
