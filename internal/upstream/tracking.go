package upstream

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/wwwyo/skillctrl/internal/jsonfmt"
	"github.com/wwwyo/skillctrl/internal/skillstate"
)

// Tracking holds skillctrl-only registrations; the native project lock keeps
// only entries that the skills CLI can restore without interpreting extensions.
const Tracking = skillstate.Lock

// PreparedTracking is the staged companion of the prepared native project lock.
const PreparedTracking = "skillctrl-lock.json"

// PreparedNative is the native-only project lock staged beside import results.
const PreparedNative = "native-skills-lock.json"

// Load reads native registrations and skillctrl metadata from the working copy.
func Load(dir string) (*Record, error) {
	native, err := loadNative(dir)
	if err != nil {
		return nil, err
	}
	private, err := skillstate.Local(dir)
	if err != nil {
		return nil, err
	}
	return combineState(native, private)
}

// Read reads both registration files from the same immutable Git tree.
func Read(dir, ref string) (*Record, error) {
	native, err := readNative(dir, ref)
	if err != nil {
		return nil, err
	}
	private, err := skillstate.Read(dir, ref)
	if err != nil {
		return nil, err
	}
	return combineState(native, private)
}

func combineState(native *Record, private *skillstate.Record) (*Record, error) {
	data, err := jsonfmt.Compact(map[string]any{"version": Version, "skills": private.Upstreams})
	if err != nil {
		return nil, err
	}
	tracking, err := parseLock(data)
	if err != nil {
		return nil, err
	}
	result, err := combine(native, tracking)
	if err != nil {
		return nil, err
	}
	result.state = private
	return result, nil
}

func sameRegistration(a, b any) bool {
	x, xerr := jsonfmt.Compact(a)
	y, yerr := jsonfmt.Compact(b)
	return xerr == nil && yerr == nil && bytes.Equal(x, y)
}

func combine(native, tracking *Record) (*Record, error) {
	result := *native
	result.Skills = maps.Clone(native.Skills)
	result.native = native
	for name, raw := range tracking.Skills {
		entry := raw.(map[string]any)
		if binding, bound := entry["native"]; bound {
			// A native edit or removal wins. Old supplemental hashes must never
			// restore a registration another manager deliberately changed.
			if !sameRegistration(native.Skills[name], binding) {
				continue
			}
			project := native.Skills[name].(map[string]any)
			if specialRegistration(name, entry) || !strings.EqualFold(field(entry, "source"), field(project, "source")) || entry["sourceType"] != project["sourceType"] || field(entry, "skillPath") != field(project, "skillPath") {
				return nil, fmt.Errorf("supplemental metadata changed native source identity: %s", name)
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
	if _, present := result["computedHash"]; !present && field(entry, "computedHash") != "" {
		result["computedHash"] = entry["computedHash"]
	}
	for _, key := range []string{"skill", "sources", "sourceLayout", "sourceCommit", "skillFolderHash", "installedAt", "updatedAt", "nativeName", "nativeExport"} {
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
	private := *record.state
	private.Upstreams = tracking.Skills
	return jsonfmt.WriteFile(filepath.Join(directory, PreparedTracking), &private)
}
