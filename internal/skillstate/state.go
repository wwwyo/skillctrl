// Package skillstate stores skillctrl provenance and explicit acceptance in one
// private lock, leaving the native skills lock to its original manager.
package skillstate

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
)

// Repository-relative locations for the current and read-compatible legacy locks.
const (
	Lock            = ".agents/skillctrl/lock.json"
	LegacyUpstreams = ".agents/skillctrl/upstreams.json"
	LegacyAccepted  = ".agents/skillctrl/intents/lock.json"
	Version         = 1
)

// Record keeps acquisition and acceptance independently writable in one file.
type Record struct {
	Version        int                        `json:"version"`
	Upstreams      map[string]any             `json:"upstreams"`
	AcceptedHashes map[string]string          `json:"acceptedHashes"`
	Extra          map[string]json.RawMessage `json:"-"`
}

// Empty returns a current record with initialized maps.
func Empty() *Record {
	return &Record{Version: Version, Upstreams: map[string]any{}, AcceptedHashes: map[string]string{}}
}

// MarshalJSON retains unknown top-level metadata across either kind of write.
func (record Record) MarshalJSON() ([]byte, error) {
	fields := maps.Clone(record.Extra)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	for key, value := range map[string]any{"version": record.Version, "upstreams": record.Upstreams, "acceptedHashes": record.AcceptedHashes} {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = data
	}
	return jsonfmt.Compact(fields)
}

// Parse validates the private lock envelope. Source validation belongs to upstream.
func Parse(data []byte) (*Record, error) {
	var record Record
	if err := json.Unmarshal(data, &record); err != nil || record.Version != Version || record.Upstreams == nil || record.AcceptedHashes == nil {
		return nil, fmt.Errorf("unsupported skillctrl lock; expected version %d", Version)
	}
	for name, entry := range record.Upstreams {
		if _, ok := entry.(map[string]any); name == "" || !ok {
			return nil, fmt.Errorf("invalid upstream skill record")
		}
	}
	if err := validateHashes(record.AcceptedHashes); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &record.Extra); err != nil {
		return nil, err
	}
	for _, key := range []string{"version", "upstreams", "acceptedHashes"} {
		delete(record.Extra, key)
	}
	return &record, nil
}

func validateHashes(hashes map[string]string) error {
	for name, hash := range hashes {
		if name == "" || hash == "" {
			return fmt.Errorf("invalid accepted skill hashes")
		}
	}
	return nil
}

func load(read func(string) ([]byte, error)) (*Record, error) {
	data, err := read(Lock)
	if err == nil {
		return Parse(data)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	record := Empty()
	data, err = read(LegacyUpstreams)
	if err == nil {
		var legacy struct {
			Version int
			Skills  map[string]any
		}
		if err := json.Unmarshal(data, &legacy); err != nil || (legacy.Version != 1 && legacy.Version != 3) || legacy.Skills == nil {
			return nil, fmt.Errorf("unsupported legacy upstream lock")
		}
		record.Upstreams = legacy.Skills
		if err := json.Unmarshal(data, &record.Extra); err != nil {
			return nil, err
		}
		delete(record.Extra, "version")
		delete(record.Extra, "skills")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	data, err = read(LegacyAccepted)
	if err == nil {
		var legacy struct {
			Version int
			Skills  map[string]string
		}
		if err := json.Unmarshal(data, &legacy); err != nil || legacy.Version != 2 || legacy.Skills == nil {
			return nil, fmt.Errorf("unsupported accepted skill lock; expected version 2")
		}
		if err := validateHashes(legacy.Skills); err != nil {
			return nil, err
		}
		record.AcceptedHashes = legacy.Skills
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return record, nil
}

func localPath(dir, relative string) (string, error) {
	path := filepath.Join(dir, filepath.FromSlash(relative))
	for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
		info, err := os.Lstat(filepath.Join(dir, parent))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("skillctrl lock requires a real directory: %s", parent)
		}
	}
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("skillctrl lock must be a regular file: %s", relative)
	}
	return path, nil
}

// Local reads the current lock, or both legacy locks without migrating them.
func Local(dir string) (*Record, error) {
	return load(func(relative string) ([]byte, error) {
		path, err := localPath(dir, relative)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	})
}

// Read reads provenance and acceptance from the same immutable Git tree.
func Read(dir, ref string) (*Record, error) {
	return load(func(relative string) ([]byte, error) {
		listing, err := gitx.Output(dir, "ls-tree", ref, "--", relative)
		if err != nil {
			return nil, err
		}
		if len(listing) == 0 {
			return nil, os.ErrNotExist
		}
		if !strings.HasPrefix(string(listing), "100644 blob ") && !strings.HasPrefix(string(listing), "100755 blob ") {
			return nil, fmt.Errorf("skillctrl lock must be a regular file: %s", relative)
		}
		return gitx.Output(dir, "show", ref+":"+relative)
	})
}

// Write publishes a complete lock before removing the legacy files. An unchanged
// canonical lock retains its bytes; migration never advances accepted hashes.
func Write(dir string, record *Record) error {
	paths := map[string]string{}
	for _, relative := range []string{Lock, LegacyUpstreams, LegacyAccepted} {
		path, err := localPath(dir, relative)
		if err != nil {
			return err
		}
		paths[relative] = path
	}
	data, err := jsonfmt.File(record)
	if err != nil {
		return err
	}
	if _, err := Parse(data); err != nil {
		return err
	}
	path := paths[Lock]
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var a, b any
	if err != nil || json.Unmarshal(existing, &a) != nil || json.Unmarshal(data, &b) != nil || !sameJSON(a, b) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".lock-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if err := file.Chmod(0o644); err != nil {
			file.Close()
			return err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := os.Rename(file.Name(), path); err != nil {
			return err
		}
	}
	for _, relative := range []string{LegacyUpstreams, LegacyAccepted} {
		if err := os.Remove(paths[relative]); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ValidateDestination refuses unsafe lock paths before an importer changes skills.
func ValidateDestination(dir string) error {
	for _, relative := range []string{Lock, LegacyUpstreams, LegacyAccepted} {
		if _, err := localPath(dir, relative); err != nil {
			return err
		}
	}
	return nil
}

func sameJSON(a, b any) bool {
	x, _ := jsonfmt.Compact(a)
	y, _ := jsonfmt.Compact(b)
	return string(x) == string(y)
}
