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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
	"github.com/wwwyo/skillctrl/internal/upstream"
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

// ValidatePaths accepts staged edits only within selected skill directories, and
// only as ordinary tracked content.
//
// A path outside the selection - an intent file, a lock, a workflow, an
// unrelated skill - is a scope violation by the reviewer, not a change to
// reconcile. Inside the selection the object type matters too: a symlink or a
// gitlink staged by a reviewer would let a later read or write escape the skill
// directory, and Git rules staged alongside would change what the accepted hash
// is computed over. Both are refused.
//
// Only paths the reviewer actually staged are examined. An untouched file that
// happens to live inside a skill directory is not the reviewer's doing and is
// left alone.
func ValidatePaths(dir string, plan lock.Plan) error {
	return validatePathsAtTree(dir, plan, "")
}

func treeDiff(tree string) []string {
	if tree == "" {
		return []string{"--cached"}
	}
	return []string{"HEAD", tree}
}

func validatePathsAtTree(dir string, plan lock.Plan, tree string) error {
	args := append([]string{"diff", "--raw", "-z", "--no-renames"}, treeDiff(tree)...)
	out, err := gitx.Output(dir, args...)
	if err != nil {
		return err
	}
	prefixes := make([]string, 0, len(plan.Skills))
	for _, name := range plan.Skills {
		prefixes = append(prefixes, lock.Skills+name+"/")
	}
	fields := strings.Split(string(out), "\x00")
	for index := 0; index+1 < len(fields); index += 2 {
		header, path := fields[index], fields[index+1]
		if path == "" {
			continue
		}
		if !hasPrefix(path, prefixes) {
			return fmt.Errorf("repair changed a path outside selected skills: %s", path)
		}
		if err := checkEntry(header, path); err != nil {
			return err
		}
	}
	return gitx.Run(dir, append([]string{"diff", "--check"}, treeDiff(tree)...)...)
}

// readBounded reads at most limit bytes and reports one byte more. Reading a
// model artifact whole would let a runaway output cost memory before anything
// checks it; the extra byte is what distinguishes "at the limit" from "over it".
func readBounded(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return data, errTooLarge
	}
	return data, nil
}

// errTooLarge marks a bounded read that hit its limit.
var errTooLarge = errors.New("artifact exceeds its size limit")

// checkEntry refuses a staged object that is not ordinary file content. The raw
// header is ":<old mode> <new mode> <old id> <new id> <status>".
func checkEntry(header, path string) error {
	fields := strings.Fields(strings.TrimPrefix(header, ":"))
	if len(fields) < 2 {
		return fmt.Errorf("unreadable staged entry: %s", path)
	}
	switch mode := fields[1]; mode {
	case "100644", "100755":
		// A removal reports no new object. Refusing it would make deleting a
		// reference inside a reviewed skill impossible.
	case "000000":
	default:
		return fmt.Errorf("repair staged a non-regular entry inside a skill: %s", path)
	}
	for _, component := range strings.Split(path, "/") {
		if isGitControlName(component) {
			return fmt.Errorf("repair staged Git rules inside a skill: %s", path)
		}
	}
	return nil
}

// isGitControlName reports whether a file name is one Git treats specially. The
// comparison folds case because a case-insensitive filesystem accepts
// ".GITIGNORE" as ".gitignore" and would let the same rules through under a
// different spelling.
func isGitControlName(name string) bool {
	for _, control := range []string{".git", ".gitignore", ".gitattributes"} {
		if strings.EqualFold(name, control) {
			return true
		}
	}
	return false
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
	rendered, err := toolchain.Render(configuration)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "mise.toml"), rendered, 0o644)
}

// ValidateSecret rejects credential bytes in reports, results, patches, and
// changed staged blobs. A transformed credential is not detectable; this catches
// the direct case and is paired with the sandbox's own log redaction.
func ValidateSecret(dir, directory string, patch []byte, secret string) error {
	return validateSecretAtTree(dir, directory, patch, secret, "")
}

func validateSecretAtTree(dir, directory string, patch []byte, secret, tree string) error {
	if secret == "" {
		return fmt.Errorf("missing inference credential")
	}
	key := []byte(secret)
	limits := map[string]int{ReportFile: MaxReportBytes, ResultFile: MaxPatchBytes}
	for _, name := range []string{ReportFile, ResultFile} {
		// Bounded reads: the credential check must not be the thing that loads a
		// runaway artifact into memory, and it must run on exactly what would be
		// exported rather than on a re-read of something else.
		data, err := readBounded(filepath.Join(directory, name), limits[name])
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if errors.Is(err, errTooLarge) {
				return fmt.Errorf("%s exceeds its size limit; artifact export refused", name)
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
	args := append([]string{"diff", "--name-only", "-z", "--no-renames", "--diff-filter=ACMRT"}, treeDiff(tree)...)
	out, err := gitx.Output(dir, args...)
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		scanner := secretScanner{key: key}
		if err := gitx.Stream(dir, &scanner, "show", tree+":"+raw); err != nil {
			return err
		}
		if scanner.found {
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
	tree, err := gitx.Output(dir, "write-tree")
	if err != nil {
		return nil, err
	}
	return acceptedAtTree(dir, plan, directory, gitx.Trimmed(tree))
}

func acceptedAtTree(dir string, plan lock.Plan, directory, tree string) ([]string, error) {
	data, err := readBounded(filepath.Join(directory, ResultFile), MaxPatchBytes)
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
	current, err := lock.Snapshot(dir, tree)
	if err != nil {
		return nil, err
	}
	for _, name := range plan.ReviewSkills {
		before, after := plan.InputTrees[name], current.Skills[name]
		if before == "" || after == "" {
			continue
		}
		original, err := gitx.Output(dir, "ls-tree", before, "--", upstream.SourceDirectory)
		if err != nil {
			return nil, err
		}
		candidate, err := gitx.Output(dir, "ls-tree", after, "--", upstream.SourceDirectory)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(original, candidate) {
			return nil, fmt.Errorf("repair changed immutable upstream originals: %s", name)
		}
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
	tree, err := gitx.Output(dir, "write-tree")
	if err != nil {
		return err
	}
	_, err = writeRepairArtifactAtTree(dir, plan, directory, secret, gitx.Trimmed(tree))
	return err
}

func writeRepairArtifactAtTree(dir string, plan lock.Plan, directory, secret, tree string) ([]string, error) {
	if err := validatePathsAtTree(dir, plan, tree); err != nil {
		return nil, err
	}
	accepted, err := acceptedAtTree(dir, plan, directory, tree)
	if err != nil {
		return nil, err
	}
	output := cappedOutput{remaining: MaxPatchBytes}
	err = gitx.Stream(dir, &output, append([]string{"diff", "--binary", "--full-index", "--no-ext-diff", "--no-renames"}, treeDiff(tree)...)...)
	if output.exceeded {
		return nil, fmt.Errorf("repair patch exceeds 5 MB")
	}
	if err != nil {
		return nil, err
	}
	patch := output.buffer.Bytes()
	// Every artifact is bounded before it is examined or written. The checks are
	// independent of the credential check and never replace it: a size refusal
	// must not become a way to skip screening.
	if err := checkArtifactBounds(directory, patch); err != nil {
		return nil, err
	}
	if err := validateSecretAtTree(dir, directory, patch, secret, tree); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, PatchFile), patch, 0o644); err != nil {
		return nil, err
	}
	return accepted, nil
}

// checkArtifactBounds refuses model output that is too large to process or to
// publish. The completion result is given the generous patch limit because the
// reviewer answers with one entry per selected skill, not with content.
func checkArtifactBounds(directory string, patch []byte) error {
	if len(patch) > MaxPatchBytes {
		return fmt.Errorf("repair patch exceeds 5 MB")
	}
	limits := map[string]int{
		ReportFile: MaxReportBytes,
		ResultFile: MaxPatchBytes,
	}
	for _, name := range []string{ReportFile, ResultFile} {
		file, err := os.Open(filepath.Join(directory, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		// One byte beyond the limit is what distinguishes "at the limit" from
		// "over it"; the file itself is never fully read.
		size, err := io.Copy(io.Discard, io.LimitReader(file, int64(limits[name])+1))
		file.Close()
		if err != nil {
			return err
		}
		if size > int64(limits[name]) {
			if name == ResultFile {
				return fmt.Errorf("adaptation result exceeds 5 MB")
			}
			return fmt.Errorf("review report exceeds 40 KB")
		}
	}
	return nil
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
	patch, patchErr := readBounded(patchPath, MaxPatchBytes)
	if plan.NeedsReview {
		if patchErr != nil {
			return fmt.Errorf("review artifact is incomplete")
		}
		if _, err := os.Stat(filepath.Join(directory, ReportFile)); err != nil {
			return fmt.Errorf("review artifact is incomplete")
		}
	}
	if !plan.NeedsReview && patchErr == nil && len(patch) > 0 {
		// Nothing was reviewed, so there is nothing a patch could legitimately
		// repair. Applying one anyway would let a stale artifact from an earlier
		// run change skills this run never selected.
		return fmt.Errorf("repair patch was supplied for a run with no review")
	}
	if errors.Is(patchErr, errTooLarge) {
		return fmt.Errorf("repair patch exceeds 5 MB")
	}
	if patchErr != nil && !errors.Is(patchErr, os.ErrNotExist) {
		return patchErr
	}
	// A refused repair must not leave anything staged. A patch is untrusted input
	// and may touch any path, so the whole index is saved before applying it and
	// restored on refusal: guessing which paths a rejected patch staged would be
	// exactly the assumption that makes this check worthless.
	saved, err := saveIndex(dir)
	if err != nil {
		return err
	}
	// The saved copy is needed only while this call can refuse.
	defer os.Remove(saved)
	if patchErr == nil && len(patch) > 0 {
		if err := gitx.Run(dir, "apply", "--cached", "--binary", patchPath); err != nil {
			return errors.Join(err, restoreIndex(dir, saved))
		}
	}
	if err := validateAndRecord(dir, plan, directory); err != nil {
		return errors.Join(err, restoreIndex(dir, saved))
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

// restoreIndex puts the saved index back exactly as it was. A failed rollback is
// reported: silently leaving a rejected patch staged would be the one outcome
// worse than the refusal that caused it.
func restoreIndex(dir string, saved string) error {
	if saved == "" {
		return nil
	}
	out, err := gitx.Output(dir, "rev-parse", "--git-path", "index")
	if err != nil {
		return err
	}
	path := gitx.Trimmed(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	data, err := os.ReadFile(saved)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
	// A reviewed skill only advances if the reviewer accepted it.
	for _, name := range plan.ReviewSkills {
		delete(names, name)
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
	registered, err := upstream.Read(dir, plan.Head)
	if err != nil {
		return err
	}
	intents, err := lock.IntentsAt(dir, plan.Head)
	if err != nil {
		return err
	}
	if _, err := lock.Record(dir, current, recorded, ordered, lock.IntentRegistered(registered.ManagedSkills(), intents)); err != nil {
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
	data, err := readBounded(filepath.Join(directory, ReportFile), MaxReportBytes)
	if errors.Is(err, os.ErrNotExist) {
		text = "Recorded skill hashes for skills without a saved intent."
	} else if err != nil {
		return "", err
	} else {
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
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
