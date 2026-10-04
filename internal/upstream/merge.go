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
const SourceDirectory = ".skillctrl-sources"

// Input identifies one GitHub skill contributing to a merged skill.
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

func containsSourceDirectory(path string) bool {
	for _, component := range strings.Split(path, "/") {
		if strings.EqualFold(component, SourceDirectory) {
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
	if err := os.RemoveAll(filepath.Join(target, SourceDirectory)); err != nil {
		return nil, err
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
		relative := name + "/" + SourceDirectory + "/" + strconv.Itoa(index)
		exported, err := adapter.Export(ExportRequest{Destination: dir, Source: input.Source, Skill: input.Skill, Name: relative, Target: filepath.Join(target, SourceDirectory, strconv.Itoa(index)), Directory: directory, Previous: force})
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
	if !configure {
		_, err := os.Stat(manifest)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		configure = os.IsNotExist(err)
	}
	if configure {
		var routing strings.Builder
		fmt.Fprintf(&routing, "---\nname: %s\ndescription: Route tasks to the registered upstream skills.\n---\n\n# %s\n\nChoose the applicable skills from the linked manifests below and follow their instructions. Resolve each skill's relative resources from its own source directory. When several skills apply, use each applicable skill; surface conflicting instructions rather than silently choosing one.\n\n", name, name)
		for index, input := range inputs {
			fmt.Fprintf(&routing, "- [%s](%s/%d/SKILL.md) — %s:%s\n", input.Skill, SourceDirectory, index, input.Source, input.Skill)
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
	prefix := ".agents/skills/" + name + "/" + SourceDirectory + "/"
	children, err := gitx.Output(dir, "ls-tree", tree+":"+strings.TrimSuffix(prefix, "/"))
	if err != nil {
		return fmt.Errorf("imported source snapshots do not match the sources array: %s", name)
	}
	snapshots := map[string]string{}
	for _, row := range strings.Split(strings.TrimSpace(string(children)), "\n") {
		metadata, child, found := strings.Cut(row, "\t")
		fields := strings.Fields(metadata)
		if !found || len(fields) != 3 || fields[0] != "040000" || fields[1] != "tree" {
			return fmt.Errorf("imported source snapshot is not a directory: %s", name)
		}
		snapshots[child] = fields[2]
	}
	if len(snapshots) != len(newSources) {
		return fmt.Errorf("imported source snapshots do not match the sources array: %s", name)
	}
	for index, value := range newSources {
		old := oldSources[index]
		if field(value, "source") != field(old, "source") || field(value, "skill") != field(old, "skill") {
			return fmt.Errorf("scheduled update changed upstream identity: %s", name)
		}
		if snapshots[strconv.Itoa(index)] == "" || snapshots[strconv.Itoa(index)] != field(value, "skillFolderHash") {
			return fmt.Errorf("imported source differs from its original hash: %s", name)
		}
	}
	out, err := gitx.Output(dir, "diff", "--name-only", "-z", "--no-renames", base, tree, "--", ".agents/skills/"+name+"/")
	if err != nil {
		return err
	}
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" && !strings.HasPrefix(path, prefix) {
			return fmt.Errorf("upstream import changed merged output: %s", name)
		}
	}
	return nil
}
