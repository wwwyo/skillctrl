package upstream

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
)

// Tracking holds skillctrl-only registrations; the native project lock keeps
// only entries that the skills CLI can restore without interpreting extensions.
const Tracking = ".agents/skillctrl/upstreams.json"

// PreparedTracking is the staged companion of the prepared native project lock.
const PreparedTracking = "skillctrl-upstreams.json"

// PreparedNative is the native-only project lock staged beside import results.
const PreparedNative = "native-skills-lock.json"

// Load reads native registrations and skillctrl metadata from the working copy.
func Load(dir string) (*Record, error) {
	native, err := loadNative(dir)
	if err != nil {
		return nil, err
	}
	for _, relative := range []string{".agents", ".agents/skillctrl"} {
		info, err := os.Lstat(filepath.Join(dir, relative))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("upstream tracking requires a real directory: %s", relative)
		}
	}
	path := filepath.Join(dir, filepath.FromSlash(Tracking))
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return combine(native, nil)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("upstream tracking must be a regular file: %s", Tracking)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tracking, err := parseLock(data)
	if err != nil {
		return nil, err
	}
	return combine(native, tracking)
}

// Read reads both registration files from the same immutable Git tree.
func Read(dir, ref string) (*Record, error) {
	native, err := readNative(dir, ref)
	if err != nil {
		return nil, err
	}
	listing, err := gitx.Output(dir, "ls-tree", ref, "--", Tracking)
	if err != nil {
		return nil, err
	}
	if len(listing) == 0 {
		return combine(native, nil)
	}
	if !strings.HasPrefix(string(listing), "100644 blob ") && !strings.HasPrefix(string(listing), "100755 blob ") {
		return nil, fmt.Errorf("upstream tracking must be a regular file: %s", Tracking)
	}
	data, err := gitx.Output(dir, "show", ref+":"+Tracking)
	if err != nil {
		return nil, err
	}
	tracking, err := parseLock(data)
	if err != nil {
		return nil, err
	}
	return combine(native, tracking)
}

func sameRegistration(a, b any) bool {
	x, xerr := jsonfmt.Compact(a)
	y, yerr := jsonfmt.Compact(b)
	return xerr == nil && yerr == nil && bytes.Equal(x, y)
}

func combine(native, tracking *Record) (*Record, error) {
	result := *native
	result.Skills = maps.Clone(native.Skills)
	result.native, result.tracking = native, tracking
	if tracking == nil {
		return &result, nil
	}
	for name, raw := range tracking.Skills {
		entry := raw.(map[string]any)
		if binding, bound := entry["native"]; bound {
			// A native edit or removal wins. Old supplemental hashes must never
			// restore a registration another manager deliberately changed.
			if !sameRegistration(native.Skills[name], binding) {
				continue
			}
			entry = maps.Clone(entry)
			delete(entry, "native")
			entry["nativeExport"] = binding
		} else if _, exists := native.Skills[name]; exists {
			return nil, fmt.Errorf("native and skillctrl registrations collide: %s", name)
		}
		result.Skills[name] = entry
	}
	return &result, nil
}

func specialRegistration(name string, entry map[string]any) bool {
	return entry["sources"] != nil || field(entry, "skill") != "" && field(entry, "skill") != name || field(entry, "nativeName") != "" && field(entry, "nativeName") != name
}

// nativeEntry projects a fresh ordinary registration onto the native v1
// contract. Preserve unknown existing fields; never add skillctrl-only fields.
func nativeEntry(entry, previous map[string]any) map[string]any {
	result := maps.Clone(previous)
	if result == nil {
		result = map[string]any{}
	}
	source := entry
	if exported, ok := entry["nativeExport"].(map[string]any); ok {
		source = exported
	}
	for _, key := range []string{"source", "sourceType", "sourceUrl", "ref", "skillPath", "computedHash"} {
		delete(result, key)
		if value, present := source[key]; present && value != "" {
			result[key] = value
		}
	}
	for _, key := range []string{"skill", "sources", "sourceLayout"} {
		delete(result, key)
	}
	return result
}

func writeRecord(path string, value, previous *Record) error {
	var data []byte
	if previous != nil && len(previous.original) > 0 && sameRegistration(value, previous) {
		data = previous.original
	} else {
		var err error
		data, err = jsonfmt.File(value)
		if err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

func (record *Record) writePrepared(directory, combinedPath string) error {
	native := &Record{Version: record.Version, Skills: map[string]any{}, Extra: maps.Clone(record.Extra)}
	tracking := &Record{Version: Version, Skills: map[string]any{}}
	if record.tracking != nil {
		tracking.Extra = maps.Clone(record.tracking.Extra)
	}
	for name, raw := range record.Skills {
		entry := raw.(map[string]any)
		previous, _ := record.native.Skills[name].(map[string]any)
		clean := maps.Clone(entry)
		delete(clean, "nativeExport")
		if specialRegistration(name, entry) {
			tracking.Skills[name] = clean
			continue
		}
		if field(entry, "sourceType") != "github" {
			native.Skills[name] = clean
			continue
		}
		project := nativeEntry(entry, previous)
		native.Skills[name] = project
		if !sameRegistration(clean, project) {
			clean["native"] = project
			tracking.Skills[name] = clean
		}
	}
	if err := writeRecord(filepath.Join(directory, PreparedNative), native, record.native); err != nil {
		return err
	}
	if err := writeRecord(combinedPath, record, nil); err != nil {
		return err
	}
	if len(tracking.Skills) > 0 || record.tracking != nil {
		return writeRecord(filepath.Join(directory, PreparedTracking), tracking, record.tracking)
	}
	return nil
}

// ValidateTracking checks that private metadata can be imported as a record.
func ValidateTracking(data []byte) error {
	_, err := parseLock(data)
	return err
}
