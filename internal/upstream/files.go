package upstream

import (
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
)

// readBlobs reads a batch of blobs in one Git process. A batch is used because
// one process per file would make importing a large skill slow, and because a
// single --batch stream keeps the object identity check in one place.
func readBlobs(repo string, identifiers []string) (map[string][]byte, error) {
	ordered := unique(slices.Values(identifiers))
	if len(ordered) == 0 {
		return map[string][]byte{}, nil
	}
	var request strings.Builder
	for _, oid := range ordered {
		request.WriteString(oid + "\n")
	}
	out, err := gitx.SafeStdin(repo, []byte(request.String()), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	result := map[string][]byte{}
	offset := 0
	for _, oid := range ordered {
		end := strings.IndexByte(string(out[offset:]), '\n')
		if end < 0 {
			return nil, fmt.Errorf("upstream object is not a blob")
		}
		header := strings.Fields(string(out[offset : offset+end]))
		if len(header) != 3 || header[0] != oid || header[1] != "blob" {
			return nil, fmt.Errorf("upstream object is not a blob")
		}
		offset += end + 1
		var size int
		if _, err := fmt.Sscanf(header[2], "%d", &size); err != nil {
			return nil, fmt.Errorf("upstream object is not a blob")
		}
		if offset+size > len(out) {
			return nil, fmt.Errorf("upstream object is not a blob")
		}
		result[oid] = out[offset : offset+size]
		offset += size + 1
	}
	return result, nil
}

func unique(values iter.Seq[string]) []string {
	seen := map[string]bool{}
	ordered := []string{}
	for value := range values {
		if !seen[value] {
			seen[value] = true
			ordered = append(ordered, value)
		}
	}
	sort.Strings(ordered)
	return ordered
}

// copyTree copies a directory tree without following symlinks. Callers validate
// the destination beforehand, so a link here would be an internal error rather
// than attacker-controlled input.
func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		case entry.Type()&os.ModeSymlink != 0:
			return fmt.Errorf("installer isolation requires symlink-free skills: %s", relative)
		default:
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unexpected non-regular skill file: %s", relative)
			}
			data, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
	})
}
