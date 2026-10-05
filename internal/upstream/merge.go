package upstream

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
)

// SourceDirectory holds immutable originals inside a merged skill. Keeping the
// originals in the skill tree binds review and CI to the same fetched inputs.
const SourceDirectory = "references"

// LegacySourceDirectory is the original numeric snapshot layout.
const LegacySourceDirectory = ".skillctrl-sources"

// Input identifies one GitHub skill to import or combine into a routing skill.
type Input struct {
	Source string `json:"source"`
	Skill  string `json:"skill"`
}

// ParseInputs parses repeatable owner/repo:skill arguments without shell evaluation.
func ParseInputs(values []string) ([]Input, error) {
	inputs := make([]Input, 0, len(values))
	for _, value := range values {
		at := strings.LastIndexByte(value, ':')
		if at < 0 {
			return nil, fmt.Errorf("upstream input must be owner/repo:skill")
		}
		identifier, err := Source(value[:at])
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, Input{Source: identifier, Skill: value[at+1:]})
	}
	if err := validateInputs(inputs); err != nil {
		return nil, err
	}
	if _, err := referencePaths(inputs); err != nil {
		return nil, err
	}
	return inputs, nil
}

func validateInputs(inputs []Input) error {
	if len(inputs) == 0 {
		return fmt.Errorf("at least one upstream input is required")
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		identifier, err := Source(input.Source)
		if err != nil {
			return err
		}
		if identifier != input.Source || !Name(input.Skill) {
			return fmt.Errorf("invalid upstream input")
		}
		key := strings.ToLower(input.Source) + ":" + input.Skill
		if seen[key] {
			return fmt.Errorf("duplicate upstream input: %s", key)
		}
		seen[key] = true
	}
	return nil
}

func sources(entry map[string]any, name string) ([]map[string]any, error) {
	if raw, ok := entry["sources"]; ok {
		if _, mixed := entry["source"]; mixed {
			return nil, fmt.Errorf("upstream entry mixes source and sources: %s", name)
		}
		array, ok := raw.([]any)
		if !ok || len(array) == 0 {
			return nil, fmt.Errorf("invalid upstream sources array: %s", name)
		}
		values := make([]map[string]any, 0, len(array))
		inputs := make([]Input, 0, len(array))
		for _, raw := range array {
			value, ok := raw.(map[string]any)
			if !ok || value["sourceType"] != "github" {
				return nil, fmt.Errorf("only GitHub upstream sources are supported: %s", name)
			}
			inputs = append(inputs, Input{Source: field(value, "source"), Skill: field(value, "skill")})
			values = append(values, value)
		}
		return values, validateInputs(inputs)
	}
	if entry["sourceType"] != "github" {
		return nil, fmt.Errorf("only GitHub upstream sources are supported: %s", name)
	}
	if _, err := Source(field(entry, "source")); err != nil {
		return nil, err
	}
	if raw, ok := entry["skill"]; ok {
		skill, ok := raw.(string)
		if !ok || !Name(skill) {
			return nil, fmt.Errorf("invalid upstream skill identity: %s", name)
		}
	}
	return []map[string]any{entry}, nil
}

func referencePaths(inputs []Input) ([]string, error) {
	paths := make([]string, len(inputs))
	seen := map[string]bool{}
	for index, input := range inputs {
		key := strings.ToLower(input.Skill)
		if seen[key] {
			return nil, fmt.Errorf("upstream skill names collide: %s", input.Skill)
		}
		seen[key] = true
		paths[index] = SourceDirectory + "/" + input.Skill
	}
	return paths, nil
}

// SourcePaths returns registered snapshot locations relative to the merged skill.
// Existing registrations without a layout marker retain their numeric locations.
func SourcePaths(entry map[string]any, name string) ([]string, error) {
	values, err := sources(entry, name)
	if err != nil {
		return nil, err
	}
	layout, marked := entry["sourceLayout"]
	if marked && layout != SourceDirectory {
		return nil, fmt.Errorf("invalid source layout: %s", name)
	}
	if marked {
		if entry["sources"] == nil {
			return nil, fmt.Errorf("source layout requires merged inputs: %s", name)
		}
		inputs := make([]Input, len(values))
		for index, value := range values {
			inputs[index] = Input{Skill: field(value, "skill")}
		}
		return referencePaths(inputs)
	}
	paths := make([]string, len(values))
	for index := range values {
		paths[index] = LegacySourceDirectory + "/" + strconv.Itoa(index)
	}
	return paths, nil
}

func containsSourceDirectory(path string) bool {
	for _, component := range strings.Split(path, "/") {
		if strings.EqualFold(component, SourceDirectory) || strings.EqualFold(component, LegacySourceDirectory) {
			return true
		}
	}
	return false
}

func mergeSkill(dir, name, target string, previous map[string]any, inputs []Input, adapter Adapter, directory string) (map[string]any, error) {
	configure := inputs != nil
	var err error
	var original []map[string]any
	if len(previous) != 0 {
		original, err = sources(previous, name)
		if err != nil {
			return nil, err
		}
	}
	if inputs == nil {
		for _, value := range original {
			inputs = append(inputs, Input{Source: field(value, "source"), Skill: field(value, "skill")})
		}
	}
	paths := make([]string, len(inputs))
	if configure {
		paths, err = referencePaths(inputs)
		if err != nil {
			return nil, err
		}
	} else {
		paths, err = SourcePaths(previous, name)
		if err != nil {
			return nil, err
		}
	}
	owned := map[string]bool{}
	if len(original) > 0 {
		oldPaths, err := SourcePaths(previous, name)
		if err != nil {
			return nil, err
		}
		for _, path := range oldPaths {
			owned[path] = true
		}
	}
	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(target, path)); err == nil && !owned[path] {
			return nil, fmt.Errorf("refusing to overwrite an unregistered reference: %s", path)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	for path := range owned {
		if err := os.RemoveAll(filepath.Join(target, path)); err != nil {
			return nil, err
		}
	}
	if len(original) > 0 && previous["sourceLayout"] == nil {
		if err := os.RemoveAll(filepath.Join(target, LegacySourceDirectory)); err != nil {
			return nil, err
		}
	}
	array := make([]any, 0, len(inputs))
	for index, input := range inputs {
		prior := map[string]any{}
		for _, value := range original {
			skill := field(value, "skill")
			if skill == "" {
				skill = name
			}
			if strings.EqualFold(field(value, "source"), input.Source) && skill == input.Skill {
				prior = value
				break
			}
		}
		// Each original is exported afresh into the isolated preparation tree.
		// Reusing the same-original shortcut here would trust a locally modified
		// source snapshot; it is the merged output that must stay unchanged.
		force := maps.Clone(prior)
		delete(force, "skillFolderHash")
		delete(force, "computedHash")
		relative := name + "/" + paths[index]
		exported, err := adapter.Export(ExportRequest{Destination: dir, Source: input.Source, Skill: input.Skill, Name: relative, Target: filepath.Join(target, paths[index]), Directory: directory, Previous: force})
		if err != nil {
			return nil, err
		}
		if field(prior, "skillPath") == field(exported, "skillPath") &&
			field(prior, "skillFolderHash") == field(exported, "skillFolderHash") {
			exported = maps.Clone(prior)
		}
		exported["skill"] = input.Skill
		array = append(array, exported)
	}
	manifest := filepath.Join(target, "SKILL.md")
	writeRouting := configure
	if !writeRouting {
		_, err := os.Stat(manifest)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		writeRouting = os.IsNotExist(err)
	}
	if writeRouting {
		var routing strings.Builder
		fmt.Fprintf(&routing, "---\nname: %s\ndescription: Route tasks to the registered upstream skills.\n---\n\n# %s\n\nChoose the applicable skills from the linked manifests below and follow their instructions. Resolve each skill's relative resources from its own source directory. When several skills apply, use each applicable skill; surface conflicting instructions rather than silently choosing one.\n\n", name, name)
		for index, input := range inputs {
			fmt.Fprintf(&routing, "- [%s](%s/SKILL.md) — %s:%s\n", input.Skill, paths[index], input.Source, input.Skill)
		}
		seed := routing.String()
		if err := os.WriteFile(manifest, []byte(seed), 0o644); err != nil {
			return nil, err
		}
	}
	if err := rejectIgnored(dir, name, map[string]treeFile{"SKILL.md": {}}); err != nil {
		return nil, err
	}
	result := maps.Clone(previous)
	for _, key := range []string{"skill", "source", "sourceType", "sourceUrl", "skillPath", "skillFolderHash", "computedHash", "sourceCommit", "installedAt", "updatedAt"} {
		delete(result, key)
	}
	result["sources"] = array
	if configure {
		result["sourceLayout"] = SourceDirectory
	}
	return result, nil
}

// ValidateMergedImport checks source identities, original hashes, and output
// preservation independently of the process that fetched scheduled inputs.
func ValidateMergedImport(dir, base, tree, name string, previous, current map[string]any) error {
	oldSources, err := sources(previous, name)
	if err != nil {
		return err
	}
	newSources, err := sources(current, name)
	if err != nil {
		return err
	}
	if _, ok := current["sources"]; !ok {
		return fmt.Errorf("scheduled update changed upstream identity: %s", name)
	}
	if _, ok := previous["sources"]; !ok || len(oldSources) != len(newSources) {
		return fmt.Errorf("scheduled update changed upstream identity: %s", name)
	}
	if current["sourceLayout"] != previous["sourceLayout"] {
		return fmt.Errorf("scheduled update changed source layout: %s", name)
	}
	paths, err := SourcePaths(current, name)
	if err != nil {
		return err
	}
	prefix := ".agents/skills/" + name + "/"
	arguments := []string{"ls-tree", "-z", tree, "--"}
	for _, path := range paths {
		arguments = append(arguments, prefix+path)
	}
	listing, err := gitx.Output(dir, arguments...)
	if err != nil {
		return err
	}
	snapshots := map[string]treeFile{}
	for _, row := range strings.Split(string(listing), "\x00") {
		if row == "" {
			continue
		}
		metadata, path, found := strings.Cut(row, "\t")
		fields := strings.Fields(metadata)
		if !found || len(fields) != 3 {
			return fmt.Errorf("invalid imported source snapshot: %s", name)
		}
		snapshots[path] = treeFile{mode: fields[0], kind: fields[1], oid: fields[2]}
	}
	for index, value := range newSources {
		old := oldSources[index]
		if field(value, "source") != field(old, "source") || field(value, "skill") != field(old, "skill") {
			return fmt.Errorf("scheduled update changed upstream identity: %s", name)
		}
		snapshot, found := snapshots[prefix+paths[index]]
		if !found || snapshot.oid != field(value, "skillFolderHash") {
			return fmt.Errorf("imported source differs from its original hash: %s", name)
		}
		if snapshot.kind != "tree" || snapshot.mode != "040000" {
			return fmt.Errorf("imported source snapshot is not a directory: %s", name)
		}
	}
	out, err := gitx.Output(dir, "diff", "--name-only", "-z", "--no-renames", base, tree, "--", ".agents/skills/"+name+"/")
	if err != nil {
		return err
	}
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" {
			continue
		}
		allowed := false
		for _, snapshot := range paths {
			if strings.HasPrefix(path, prefix+snapshot+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("upstream import changed merged output: %s", name)
		}
	}
	return nil
}
