package upstream

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// origin is a fake GitHub repository reachable through git's url.insteadOf, so
// the importer's real clone path is exercised without a network dependency.
type origin struct {
	t           *testing.T
	dir         string
	gitConfig   string
	identifiers map[string]string
}

func newOrigin(t *testing.T) *origin {
	t.Helper()
	base := t.TempDir()
	o := &origin{t: t, dir: filepath.Join(base, "source"),
		gitConfig: filepath.Join(base, "gitconfig"), identifiers: map[string]string{}}
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	o.initRepo()
	return o
}

// withRepository points a fake owner/repo at a local path and returns its identifier.
func (o *origin) withRepository(identifier, path string) {
	o.identifiers[identifier] = path
	o.writeConfig()
}

func (o *origin) writeConfig() {
	content := ""
	for identifier, path := range o.identifiers {
		content += fmt.Sprintf("[url \"file://%s\"]\n\tinsteadOf = https://github.com/%s.git\n", path, identifier)
	}
	if err := os.WriteFile(o.gitConfig, []byte(content), 0o644); err != nil {
		o.t.Fatal(err)
	}
}

func (o *origin) initRepo() {
	o.t.Helper()
	o.git("init", "-q")
	o.git("config", "user.name", "Fixture")
	o.git("config", "user.email", "fixture@example.invalid")
	o.git("config", "commit.gpgsign", "false")
}

func (o *origin) git(args ...string) string {
	o.t.Helper()
	command := exec.Command("git", args...)
	command.Dir = o.dir
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+o.gitConfig, "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		o.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (o *origin) write(path, content string) {
	o.t.Helper()
	full := filepath.Join(o.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		o.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		o.t.Fatal(err)
	}
}

func (o *origin) commit(message string) string {
	o.t.Helper()
	o.git("add", "-A")
	o.git("-c", "commit.gpgsign=false", "commit", "-qm", message)
	return o.git("rev-parse", "HEAD")
}

func manifest(name, body string) string { return "---\nname: " + name + "\n---\n" + body + "\n" }

// newDestination builds a repository whose .gitignore decides what is trackable.
func newDestination(t *testing.T, ignore string) string {
	t.Helper()
	dir := t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.name", "Fixture"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		inner := exec.Command("git", args...)
		inner.Dir = dir
		if out, err := inner.CombinedOutput(); err != nil {
			t.Fatalf("config: %v\n%s", err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignore), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := exec.Command("git", "add", "-A")
	inner.Dir = dir
	if out, err := inner.CombinedOutput(); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	inner = exec.Command("git", "-c", "commit.gpgsign=false", "commit", "-qm", "base")
	inner.Dir = dir
	if out, err := inner.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	return dir
}

func withGitConfig(t *testing.T, config string) {
	t.Helper()
	previous, had := os.LookupEnv("GIT_CONFIG_GLOBAL")
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Cleanup(func() {
		if had {
			os.Setenv("GIT_CONFIG_GLOBAL", previous)
		} else {
			os.Unsetenv("GIT_CONFIG_GLOBAL")
		}
	})
}

func TestSourceRejectsAnythingButAGitHubIdentifier(t *testing.T) {
	valid := map[string]string{
		"owner/repo":                 "owner/repo",
		"https://github.com/o/r":     "o/r",
		"https://github.com/o/r.git": "o/r",
		"https://github.com/o/r/":    "o/r",
		"owner-1/repo.name_2":        "owner-1/repo.name_2",
	}
	for input, want := range valid {
		got, err := Source(input)
		if err != nil || got != want {
			t.Fatalf("Source(%q) = %q, %v", input, got, err)
		}
	}
	rejected := []string{
		"https://github.com/o/r?token=secret",
		"https://gitlab.com/o/r",
		"https://user:token@github.com/o/r",
		"../escape", "/absolute", "owner/", "owner/repo/branch", "o/r/tree/main",
		"owner/repo#fragment", "",
	}
	for _, input := range rejected {
		if _, err := Source(input); err == nil {
			t.Fatalf("Source accepted %q", input)
		}
	}
}

// TestRelocatedOriginalFollowsTheSkill covers an upstream that moved a skill to
// a new path. The recorded path is stale, but the skill is still uniquely
// identifiable, so the import follows it and records the new path and hash.
func TestRelocatedOriginalFollowsTheSkill(t *testing.T) {
	o := newOrigin(t)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	o.dir = source
	o.initRepo()
	o.withRepository("fixture/source", source)
	o.write("skills/new-skill/SKILL.md", manifest("canonical-new-skill", "upstream v1"))
	o.commit("original v1")
	tree := o.git("rev-parse", "HEAD:skills/new-skill")
	head := o.git("rev-parse", "HEAD")

	// The directory is renamed upstream while the declared name stays the same.
	if err := os.Rename(filepath.Join(source, "skills/new-skill"),
		filepath.Join(source, "skills/engineering/new-skill")); err != nil {
		os.MkdirAll(filepath.Join(source, "skills/engineering"), 0o755)
		if err := os.Rename(filepath.Join(source, "skills/new-skill"),
			filepath.Join(source, "skills/engineering/new-skill")); err != nil {
			t.Fatal(err)
		}
	}
	o.commit("move original skill")

	withGitConfig(t, o.gitConfig)
	repository, err := New("fixture/source", filepath.Join(t.TempDir(), "clone"))
	if err != nil {
		t.Fatal(err)
	}
	// Both the declared name and the directory name resolve to the same manifest.
	declared, err := repository.Select("new-skill", "")
	if err != nil {
		t.Fatal(err)
	}
	aliased, err := repository.Select("canonical-new-skill", "")
	if err != nil {
		t.Fatal(err)
	}
	if declared != aliased {
		t.Fatal("declared and directory names must resolve to the same manifest")
	}
	previous := map[string]any{
		"source": "fixture/source", "skillPath": "skills/new-skill/SKILL.md",
		"skillFolderHash": tree, "installedAt": "2026-01-01T00:00:00.000Z",
		"pluginName": "fixture-plugin",
	}
	destination := newDestination(t, "")
	entry, err := repository.Export(destination, "new-skill", filepath.Join(t.TempDir(), "exported"), previous)
	if err != nil {
		t.Fatal(err)
	}
	if entry["skillPath"] != "skills/engineering/new-skill/SKILL.md" {
		t.Fatalf("relocation was not followed: %v", entry["skillPath"])
	}
	if entry["skillFolderHash"] != tree {
		t.Fatalf("unexpected original hash: %v", entry["skillFolderHash"])
	}
	if entry["sourceCommit"] != head {
		// The move itself does not change the folder tree, but the recorded
		// commit must be the one actually fetched.
		if entry["sourceCommit"] != o.git("rev-parse", "HEAD") {
			t.Fatalf("unexpected source commit: %v", entry["sourceCommit"])
		}
	}
	if entry["installedAt"] != "2026-01-01T00:00:00.000Z" {
		t.Fatal("install time must be preserved across an update")
	}
	if entry["pluginName"] != "fixture-plugin" {
		t.Fatal("an unrelated upstream field was dropped")
	}
}

// TestRepositoryRootSkillIsTheWholeDirectory covers a repository whose root is
// the skill: every tracked file belongs to it and the whole tree is the original.
func TestRepositoryRootSkillIsTheWholeDirectory(t *testing.T) {
	o := newOrigin(t)
	source := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	o.dir = source
	o.initRepo()
	o.withRepository("fixture/root", source)
	o.write("SKILL.md", manifest("root-skill", "whole directory is the skill"))
	o.write("README.md", "included resource\n")
	o.commit("root skill")

	withGitConfig(t, o.gitConfig)
	repository, err := New("fixture/root", filepath.Join(t.TempDir(), "clone"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "exported")
	entry, err := repository.Export(newDestination(t, ""), "root-skill", target, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if entry["skillPath"] != "SKILL.md" {
		t.Fatalf("unexpected manifest path: %v", entry["skillPath"])
	}
	data, err := os.ReadFile(filepath.Join(target, "README.md"))
	if err != nil || string(data) != "included resource\n" {
		t.Fatalf("repository root skill lost its resources: %v", err)
	}
	if entry["skillFolderHash"] != o.git("rev-parse", "HEAD^{tree}") {
		t.Fatal("a repository root skill must be hashed as the whole tree")
	}
}

// TestRejectedUpstreamContent covers every input that must be refused rather
// than imported: a symlink, a submodule, Git rules that could change how the
// accepted content is tracked, and content the destination would not track.
func TestRejectedUpstreamContent(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(origin)
		ignore string
	}{
		{name: "a case-folded gitignore", setup: func(o origin) {
			o.write("skills/manual/SKILL.md", manifest("manual", "body"))
			o.write("skills/manual/.GITIGNORE", "reference.md\n")
		}},
		{name: "symlink", setup: func(o origin) {
			o.write("skills/manual/SKILL.md", manifest("manual", "body"))
			if err := os.Symlink("/tmp", filepath.Join(o.dir, "skills/manual/escape")); err != nil {
				o.t.Fatal(err)
			}
		}},
		{name: "gitignore", setup: func(o origin) {
			o.write("skills/manual/SKILL.md", manifest("manual", "body"))
			o.write("skills/manual/.gitignore", "reference.md\n")
		}},
		{name: "gitattributes", setup: func(o origin) {
			o.write("skills/manual/SKILL.md", manifest("manual", "body"))
			o.write("skills/manual/.gitattributes", "*.md text eol=lf\n")
		}},
		{name: "destination ignores a file", ignore: "*.local.*", setup: func(o origin) {
			o.write("skills/manual/SKILL.md", manifest("manual", "body"))
			o.write("skills/manual/secret.local.md", "tracked upstream, ignored downstream")
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			o := newOrigin(t)
			source := filepath.Join(t.TempDir(), "source")
			if err := os.MkdirAll(source, 0o755); err != nil {
				t.Fatal(err)
			}
			o.dir = source
			o.initRepo()
			o.withRepository("fixture/source", source)
			test.setup(*o)
			o.git("add", "-A", "--", "skills")
			o.git("add", "--force", "--", "skills")
			o.git("-c", "commit.gpgsign=false", "commit", "-qm", "original")
			destination := newDestination(t, test.ignore)
			withGitConfig(t, o.gitConfig)
			repository, err := New("fixture/source", filepath.Join(t.TempDir(), "clone"))
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "exported")
			_, err = repository.Export(destination, "manual", target, map[string]any{})
			if err == nil {
				t.Fatal("unsafe upstream content was imported")
			}
			// The target is the directory Export would have written; checking a
			// fresh temporary directory would pass no matter what Export did.
			if _, err := os.Stat(target); err == nil {
				t.Fatal("a rejected import left files behind")
			}
		})
	}
}

// TestAmbiguousSkillIsRefused keeps a name from resolving to the wrong manifest.
func TestAmbiguousSkillIsRefused(t *testing.T) {
	o := newOrigin(t)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	o.dir = source
	o.initRepo()
	o.withRepository("fixture/source", source)
	o.write("skills/one/SKILL.md", manifest("shared", "first"))
	o.write("skills/two/SKILL.md", manifest("shared", "second"))
	o.commit("ambiguous")

	withGitConfig(t, o.gitConfig)
	repository, err := New("fixture/source", filepath.Join(t.TempDir(), "clone"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Select("shared", ""); err == nil {
		t.Fatal("an ambiguous skill name was resolved arbitrarily")
	}
	if _, err := repository.Select("shared", "skills/one/SKILL.md"); err != nil {
		t.Fatalf("a recorded path must disambiguate: %v", err)
	}
	if _, err := repository.Select("absent", ""); err == nil {
		t.Fatal("a missing skill was resolved")
	}
}

// TestFindBoundsAndSkips covers the public index contract: unsupported sources
// are counted rather than fatal, the result is capped, and invalid input is
// refused before any request is made.
func TestFindBoundsAndSkips(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.String()
		fmt.Fprint(w, `{"skills":[
			{"name":"manual","source":"fixture/source","installs":12},
			{"name":"lark-skill-maker","source":"open.feishu.cn","installs":1},
			{"name":"missing-source"},
			"not an object",
			{"name":"../escape","source":"fixture/source"}]}`)
	}))
	defer server.Close()
	original := searchURL
	searchURL = server.URL + "?"
	t.Cleanup(func() { searchURL = original })

	result, err := Find("browser search", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "manual" {
		t.Fatalf("unexpected results: %v", result.Skills)
	}
	if result.Skipped != 4 {
		t.Fatalf("skipped %d want 4", result.Skipped)
	}
	if !strings.Contains(seen, "q=browser+search") || !strings.Contains(seen, "owner=fixture") {
		t.Fatalf("query was not forwarded: %s", seen)
	}
	if result.Skills[0].Installs == nil || *result.Skills[0].Installs != 12 {
		t.Fatalf("install count was lost: %v", result.Skills[0].Installs)
	}

	rejected := []struct{ query, owner string }{
		{"", ""}, {"   ", ""}, {"browser", "../owner"}, {"browser\nnewline", ""},
	}
	for _, test := range rejected {
		if _, err := Find(test.query, test.owner); err == nil {
			t.Fatalf("Find accepted %q %q", test.query, test.owner)
		}
	}
}

// TestLoadPreservesUnknownFields is the lock compatibility promise: an entry
// written by another tool keeps every field it had.
func TestLoadPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(Lock))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3,"skills":{"manual":{
		"source":"owner/repo","sourceType":"github","skillFolderHash":"abc",
		"pluginName":"p","future":{"nested":true}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	record, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := record.Skills["manual"].(map[string]any)
	if !ok || entry["pluginName"] != "p" {
		t.Fatalf("unknown fields were dropped: %v", record.Skills["manual"])
	}
	if _, ok := entry["future"].(map[string]any); !ok {
		t.Fatal("nested unknown fields were dropped")
	}
}

func TestLoadRejectsUnsupportedLocks(t *testing.T) {
	for _, content := range []string{
		`{"version":2,"skills":{}}`,
		`{"version":3}`,
		`{"version":3,"skills":{"../escape":{"sourceType":"github","source":"o/r"}}}`,
		`{"version":3,"skills":{"a":{"source":"o/r"}}}`,
		`not json`,
	} {
		dir := filepath.Join(t.TempDir(), "nested")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(Lock))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(Lock)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Fatalf("Load accepted %s", content)
		}
	}
}
