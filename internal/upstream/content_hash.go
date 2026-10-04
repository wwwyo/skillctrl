package upstream

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// contentHash follows the project lock's path-and-content SHA-256 convention.
// Its collation matches the skills CLI's localeCompare ordering, rather than
// byte ordering, which puts punctuation and letter case in a different order.
func contentHash(files map[string]treeFile, contents map[string][]byte) string {
	names := slices.Sorted(maps.Keys(files))
	order := collate.New(language.English)
	slices.SortStableFunc(names, order.CompareString)
	hash := sha256.New()
	for _, name := range names {
		skip := false
		parts := strings.Split(name, "/")
		for _, part := range parts[:len(parts)-1] {
			if part == ".git" || part == "node_modules" {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		hash.Write([]byte(name))
		hash.Write(contents[files[name].oid])
	}
	return hex.EncodeToString(hash.Sum(nil))
}
