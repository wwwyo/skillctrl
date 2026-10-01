// Package adapt runs intent review without write credentials and validates its
// patch separately.
//
// The split is the point of this package. The reviewer runs with an inference
// credential and no write token; everything it produces is treated as untrusted
// input until a second, credential-free path re-derives the plan, checks that
// the patch touched only the selected skills, checks that every reviewed skill
// appears exactly once in the accepted/unresolved partition, and refuses to
// export an artifact containing the inference credential.
package adapt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
)

// Bounds on untrusted model output. A report or patch larger than this is not
// something the reviewer should have produced, and refusing early keeps a
// runaway output from reaching GitHub or the Git index.
const (
	MaxPatchBytes  = 5_000_000
	MaxReportBytes = 40_000
)

// Paths of the artifacts exchanged between the reviewing job and the validating
// job.
const (
	PatchFile  = "repair.patch"
	ReportFile = "report.md"
	ResultFile = "result.json"
)

// commitish accepts only a full commit ID, so a plan carrying a ref name or an
// empty value cannot be resolved to something other than what was reviewed.
var commitish = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Paths inside the trusted source tree. They are configurable so the tool is
// not tied to one repository's layout, while the defaults keep working for
// existing users.
const (
	DefaultToolchainConfig = "home/dot_config/mise/config.toml"
	DefaultAgentModels     = "home/dot_pi/agent/models.json"
)

// ValidateHead binds all edits to the immutable head selected before inference.
func ValidateHead(dir string, plan lock.Plan) error {
	if !commitish.MatchString(plan.Head) {
		return fmt.Errorf("selected head is not a commit: %q", plan.Head)
	}
	head, err := gitx.Output(dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if gitx.Trimmed(head) != plan.Head {
		return fmt.Errorf("checkout HEAD differs from selected PR head")
	}
	return nil
}

// ValidatePaths accepts staged edits only within selected skill directories.
// Anything else - an intent file, a lock, a workflow, an unrelated skill - is a
// scope violation by the reviewer, not a change to reconcile.
func ValidatePaths(dir string, plan lock.Plan) error {
	out, err := gitx.Output(dir, "diff", "--cached", "--name-only", "-z", "--no-renames")
	if err != nil {
		return err
	}
	prefixes := make([]string, 0, len(plan.Skills))
	for _, name := range plan.Skills {
		prefixes = append(prefixes, lock.Skills+name+"/")
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		if !hasPrefix(raw, prefixes) {
			return fmt.Errorf("repair changed a path outside selected skills: %s", raw)
		}
	}
	return gitx.Run(dir, "diff", "--cached", "--check")
}

func hasPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// Prepare extracts the required tool pins and the release policy from the
// trusted source. Only the two tool pins and the two settings are copied:
// copying the whole configuration would also load unrelated tools and the
// encrypted environment of the maintainer's machine.
func Prepare(dir string, plan lock.Plan, source, output string) error {
	return PrepareWithPaths(dir, plan, source, output, toolchain.ConfigPath())
}

// PrepareWithPaths is Prepare with an explicit trusted-source path, used by
// repositories that keep their toolchain configuration elsewhere.
func PrepareWithPaths(dir string, plan lock.Plan, source, output, configPath string) error {
	if err := ValidateHead(dir, plan); err != nil {
		return err
	}
	configuration, err := toolchain.Trusted(dir, source, configPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "mise.toml"), toolchain.Render(configuration), 0o644)
}

// ValidateSecret rejects credential bytes in reports, results, patches, and
// changed staged blobs. A transformed credential is not detectable; this catches
// the direct case and is paired with the sandbox's own log redaction.
func ValidateSecret(dir, directory string, patch []byte, secret string) error {
	if secret == "" {
		return fmt.Errorf("missing inference credential")
	}
	key := []byte(secret)
	for _, name := range []string{ReportFile, ResultFile} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if contains(data, key) {
			return fmt.Errorf("inference credential found in review output; artifact export refused")
		}
	}
	if contains(patch, key) {
		return fmt.Errorf("inference credential found in review output; artifact export refused")
	}
	out, err := gitx.Output(dir, "diff", "--cached", "--name-only", "-z", "--no-renames", "--diff-filter=ACMRT")
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		content, err := gitx.Output(dir, "show", ":"+raw)
		if err != nil {
			return err
		}
		if contains(content, key) {
			return fmt.Errorf("inference credential found in staged content; artifact export refused")
		}
	}
	return nil
}

func contains(data, key []byte) bool {
	if len(key) == 0 || len(data) < len(key) {
		return false
	}
	return strings.Contains(string(data), string(key))
}

// Accepted requires an explicit, complete partition of the reviewed skills
// before any hash advances. A reviewer that marks everything accepted without
// evidence, or that silently omits a skill, is rejected.
func Accepted(dir string, plan lock.Plan, directory string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(directory, ResultFile))
	if err != nil {
		return nil, fmt.Errorf("invalid adaptation result")
	}
	var result struct {
		Accepted   []string `json:"accepted"`
		Unresolved []string `json:"unresolved"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid adaptation result")
	}
	names := append(append([]string{}, result.Accepted...), result.Unresolved...)
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}
	if len(names) != len(seen) || !sameSet(names, plan.ReviewSkills) {
		return nil, fmt.Errorf("adaptation result must cover exactly the reviewed skills")
	}
	tree, err := gitx.Output(dir, "write-tree")
	if err != nil {
		return nil, err
	}
	current, err := lock.Snapshot(dir, gitx.Trimmed(tree))
	if err != nil {
		return nil, err
	}
	for _, name := range result.Accepted {
		if _, ok := plan.InputTrees[name]; !ok {
			return nil, fmt.Errorf("a removed skill with a surviving intent cannot be accepted")
		}
		if _, ok := current.Skills[name]; !ok {
			return nil, fmt.Errorf("a removed skill with a surviving intent cannot be accepted")
		}
	}
	accepted := map[string]bool{}
	for _, name := range result.Accepted {
		accepted[name] = true
	}
	for _, name := range plan.Skills {
		if accepted[name] {
			continue
		}
		if current.Skills[name] != plan.InputTrees[name] {
			return nil, fmt.Errorf("repair changed an unresolved or intent-free skill: %s", name)
		}
	}
	return result.Accepted, nil
}

func sameSet(values, expected []string) bool {
	set := map[string]bool{}
	for _, value := range expected {
		set[value] = true
	}
	for _, value := range values {
		if !set[value] {
			return false
		}
	}
	return len(set) == len(values)
}

// WriteRepairArtifact stages validated skill bodies and exports a
// credential-checked binary patch.
func WriteRepairArtifact(dir string, plan lock.Plan, directory, secret string) error {
	if err := gitx.Run(dir, "add", "-A", "--", lock.Skills); err != nil {
		return err
	}
	if err := ValidatePaths(dir, plan); err != nil {
		return err
	}
	if _, err := Accepted(dir, plan, directory); err != nil {
		return err
	}
	patch, err := gitx.Output(dir, "diff", "--cached", "--binary", "--full-index", "--no-ext-diff", "--no-renames")
	if err != nil {
		return err
	}
	if err := ValidateSecret(dir, directory, patch, secret); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, PatchFile), patch, 0o644)
}

// Apply validates the artifact, then advances only the resolved and intent-free
// hashes. The patch is applied to the index only, never to the working tree:
// applying to the working tree could follow a skill symlink into the writer's
// filesystem, and the working tree must keep showing the reviewer's edit as an
// unstaged change the caller can inspect.
func Apply(dir string, plan lock.Plan, directory string) error {
	if err := ValidateHead(dir, plan); err != nil {
		return err
	}
	patchPath := filepath.Join(directory, PatchFile)
	patch, patchErr := os.ReadFile(patchPath)
	if plan.NeedsReview {
		if patchErr != nil {
			return fmt.Errorf("review artifact is incomplete")
		}
		if _, err := os.Stat(filepath.Join(directory, ReportFile)); err != nil {
			return fmt.Errorf("review artifact is incomplete")
		}
	}
	if patchErr == nil && len(patch) > MaxPatchBytes {
		return fmt.Errorf("repair patch exceeds 5 MB")
	}
	// A refused repair must not leave anything staged. A patch is untrusted input
	// and may touch any path, so the whole index is saved before applying it and
	// restored on refusal: guessing which paths a rejected patch staged would be
	// exactly the assumption that makes this check worthless.
	saved, err := saveIndex(dir)
	if err != nil {
		return err
	}
	if patchErr == nil && len(patch) > 0 {
		if err := gitx.Run(dir, "apply", "--cached", "--binary", patchPath); err != nil {
			restoreIndex(dir, saved)
			return err
		}
	}
	if err := validateAndRecord(dir, plan, directory); err != nil {
		restoreIndex(dir, saved)
		return err
	}
	return nil
}

// saveIndex copies the caller's index file aside.
func saveIndex(dir string) (string, error) {
	out, err := gitx.Output(dir, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", err
	}
	path := gitx.Trimmed(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// An unborn or freshly initialized repository may have no index yet; the
		// absence is itself the state to restore.
		return "", err
	}
	saved, err := os.CreateTemp("", "skillctrl-index-")
	if err != nil {
		return "", err
	}
	defer saved.Close()
	if _, err := saved.Write(data); err != nil {
		return "", err
	}
	return saved.Name(), nil
}

// restoreIndex puts the saved index back exactly as it was.
func restoreIndex(dir string, saved string) {
	if saved == "" {
		return
	}
	defer os.Remove(saved)
	out, err := gitx.Output(dir, "rev-parse", "--git-path", "index")
	if err != nil {
		return
	}
	path := gitx.Trimmed(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	data, err := os.ReadFile(saved)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func validateAndRecord(dir string, plan lock.Plan, directory string) error {
	if err := ValidatePaths(dir, plan); err != nil {
		return err
	}
	tree, err := gitx.Output(dir, "write-tree")
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, name := range plan.Skills {
		names[name] = true
	}
	deleteAll := map[string]bool{}
	for _, name := range plan.ReviewSkills {
		deleteAll[name] = true
	}
	for name := range names {
		if deleteAll[name] {
			delete(names, name)
		}
	}
	if plan.NeedsReview {
		accepted, err := Accepted(dir, plan, directory)
		if err != nil {
			return err
		}
		for _, name := range accepted {
			names[name] = true
		}
	}
	current, err := lock.Snapshot(dir, gitx.Trimmed(tree))
	if err != nil {
		return err
	}
	recorded, err := lock.Read(dir, plan.Head)
	if err != nil {
		return err
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	if _, err := lock.Record(dir, current, recorded, ordered); err != nil {
		return err
	}
	if err := gitx.Run(dir, "add", "--", lock.Lock); err != nil {
		return err
	}
	return gitx.Run(dir, "diff", "--cached", "--check")
}

// Export validates the edits made inside the agent sandbox before exporting
// repair artifacts. The sandbox resets to the selected head, so the plan is
// still the reference for what may have changed.
func Export(dir string, plan lock.Plan, directory, secret string) error {
	if err := ValidateHead(dir, plan); err != nil {
		return err
	}
	after, err := lock.WorkingTree(dir, ".")
	if err != nil {
		return err
	}
	out, err := gitx.Output(dir, "diff", "--name-only", "-z", "--no-renames", plan.Head, after)
	if err != nil {
		return err
	}
	prefixes := make([]string, 0, len(plan.ReviewSkills))
	for _, name := range plan.ReviewSkills {
		prefixes = append(prefixes, lock.Skills+name+"/")
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		if !hasPrefix(raw, prefixes) {
			return fmt.Errorf("reviewer changed a path outside reviewed skills: %s", raw)
		}
	}
	return WriteRepairArtifact(dir, plan, directory, secret)
}

// ReadReport bounds the report size before the model output is written to
// GitHub, and supplies an explicit note when no review happened.
func ReadReport(directory string) (string, error) {
	var text string
	data, err := os.ReadFile(filepath.Join(directory, ReportFile))
	if errors.Is(err, os.ErrNotExist) {
		text = "Recorded skill hashes for skills without a saved intent."
	} else if err != nil {
		return "", err
	} else {
		if len(data) > MaxReportBytes+1 {
			return "", fmt.Errorf("review report exceeds 40 KB")
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" || len(text) > MaxReportBytes {
		return "", fmt.Errorf("missing or oversized review report")
	}
	return text, nil
}

// Commit creates a bot commit without invoking repository hooks or signing.
func Commit(dir, message string) error {
	return gitx.Run(dir,
		"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false",
		"-c", "user.name=github-actions[bot]",
		"-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com",
		"commit", "-m", message)
}
