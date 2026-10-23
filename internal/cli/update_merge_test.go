package cli_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const originals = ".agents/skillctrl/originals"

// publishDemo commits a multi-line original with nested and executable files.
func (h *harness) publishDemo(t *testing.T, body string, extra map[string]string) {
	t.Helper()
	h.writeOrigin("skills/demo/SKILL.md", manifest("demo", body))
	h.writeOrigin("skills/demo/references/guide.md", "guide v1\n")
	h.writeOrigin("skills/demo/run.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(h.origin, "skills/demo/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range extra {
		h.writeOrigin("skills/demo/"+path, content)
	}
	h.originGit("add", "-A", "--", "skills")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "demo original")
}

const demoV1 = "line one\nline two\nline three\nline four\nline five"

// adapt replaces one line of the installed demo skill, as a local customization.
func (h *harness) adapt(t *testing.T, from, to string) {
	t.Helper()
	path := ".agents/skills/demo/SKILL.md"
	body := string(h.read(path))
	if !strings.Contains(body, from) {
		t.Fatalf("installed skill lacks %q:\n%s", from, body)
	}
	h.write(path, strings.Replace(body, from, to, 1))
}

func onlyUpdate(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	updates, ok := result["updates"].([]any)
	if !ok || len(updates) != 1 {
		t.Fatalf("expected one update report: %v", result)
	}
	return updates[0].(map[string]any)
}

func TestUpdateMergesUpstreamChangesIntoLocalAdaptation(t *testing.T) {
	h := newHarness(t)
	h.publishDemo(t, demoV1, nil)
	h.run(0, "add", "fixture/source:demo")
	if _, err := os.Stat(filepath.Join(h.root, originals)); !os.IsNotExist(err) {
		t.Fatal("add created a legacy base snapshot")
	}
	h.write(".agents/skills/demo/scripts/am.test.mjs", "local test\n")
	h.adapt(t, "line two", "line two adapted locally")
	h.commitAll()

	h.publishDemo(t, strings.Replace(demoV1, "line five", "line five from upstream v2", 1),
		map[string]string{"references/guide.md": "guide v2\n", "references/new.md": "new upstream file\n"})
	result := h.run(0, "update", "demo")

	update := onlyUpdate(t, result)
	if update["status"] != "merged" || update["base"] != "upstream-history" || update["conflicts"] != nil {
		t.Fatalf("non-conflicting update was not merged from upstream history: %v", update)
	}
	body := string(h.read(".agents/skills/demo/SKILL.md"))
	if !strings.Contains(body, "line two adapted locally") || !strings.Contains(body, "line five from upstream v2") {
		t.Fatalf("merge lost the adaptation or the upstream change:\n%s", body)
	}
	if string(h.read(".agents/skills/demo/references/new.md")) != "new upstream file\n" ||
		string(h.read(".agents/skills/demo/references/guide.md")) != "guide v2\n" {
		t.Fatal("upstream file additions or edits were not applied")
	}
	if info, err := os.Stat(filepath.Join(h.root, ".agents/skills/demo/run.sh")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("executable mode of an unchanged upstream file was lost")
	}
	changes := map[string]string{}
	for _, raw := range update["changes"].([]any) {
		change := raw.(map[string]any)
		changes[str(change["path"])] = str(change["upstream"]) + "/" + str(change["result"])
	}
	want := map[string]string{"SKILL.md": "modified/merged", "references/guide.md": "modified/applied", "references/new.md": "added/applied"}
	if len(changes) != len(want) {
		t.Fatalf("changes: got %v want %v", changes, want)
	}
	for path, value := range want {
		if changes[path] != value {
			t.Fatalf("changes: got %v want %v", changes, want)
		}
	}
	// The working-tree diff now contains only the upstream change, so a reviewer
	// does not have to tell lost adaptation apart from new upstream content.
	diff := h.git("diff", "--", ".agents/skills/demo")
	if strings.Contains(diff, "\n-line two adapted locally") || !strings.Contains(diff, "\n+line five from upstream v2") {
		t.Fatalf("skill diff is not limited to the upstream change:\n%s", diff)
	}
	if string(h.read(".agents/skills/demo/scripts/am.test.mjs")) != "local test\n" {
		t.Fatal("merge removed a local-only test file")
	}
	if h.log() != "" {
		t.Fatal("update invoked a reviewer")
	}
	h.commitAll()
	h.run(0, "remove", "demo")
	if _, err := os.Stat(filepath.Join(h.root, originals, "demo")); !os.IsNotExist(err) {
		t.Fatal("remove kept the base snapshot")
	}
}

func TestUpdateReportsConflictsWithLocations(t *testing.T) {
	h := newHarness(t)
	h.publishDemo(t, demoV1, nil)
	h.run(0, "add", "fixture/source:demo")
	h.adapt(t, "line four", "line four adapted locally")
	h.commitAll()

	h.publishDemo(t, strings.Replace(strings.Replace(demoV1, "line four", "line four rewritten upstream", 1), "line one", "line one from upstream", 1), nil)
	result := h.run(0, "update", "demo")

	update := onlyUpdate(t, result)
	if update["status"] != "conflicted" {
		t.Fatalf("overlapping edits were not reported as a conflict: %v", update)
	}
	conflicts := update["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts: %v", conflicts)
	}
	conflict := conflicts[0].(map[string]any)
	if conflict["path"] != "SKILL.md" || conflict["kind"] != "content" {
		t.Fatalf("conflict location: %v", conflict)
	}
	lines := strings.Split(string(h.read(".agents/skills/demo/SKILL.md")), "\n")
	span := conflict["lines"].([]any)[0].([]any)
	start, end := int(span[0].(float64)), int(span[1].(float64))
	if lines[start-1] != "<<<<<<< local" || lines[end-1] != ">>>>>>> new original" {
		t.Fatalf("reported lines %d-%d do not delimit the conflict:\n%s", start, end, strings.Join(lines, "\n"))
	}
	block := strings.Join(lines[start-1:end], "\n")
	for _, side := range []string{"line four adapted locally", "||||||| previous original\nline four\n", "line four rewritten upstream"} {
		if !strings.Contains(block, side) {
			t.Fatalf("conflict block lacks %q:\n%s", side, block)
		}
	}
	if lines[1] != "name: demo" || !strings.Contains(strings.Join(lines, "\n"), "line one from upstream") {
		t.Fatal("non-conflicting upstream hunk was not applied beside the conflict")
	}
}

func TestUpdateFallsBackToReplacementWithoutAVerifiedOriginal(t *testing.T) {
	h := newHarness(t)
	// A registration whose tree is no longer reachable cannot supply a base.
	registrations := h.upstreamSkills()
	registrations["manual"].(map[string]any)["skillFolderHash"] = strings.Repeat("1", 40)
	h.write("skills-lock.json", mustJSON(map[string]any{"version": 3, "skills": registrations}))
	h.commitAll()
	h.publishSecondVersion(t)
	result := h.run(0, "update", "manual")

	update := onlyUpdate(t, result)
	if update["status"] != "replaced" || update["base"] != nil {
		t.Fatalf("missing base was not reported as a replacement: %v", update)
	}
	reason := str(update["reason"])
	for _, part := range []string{"not in the repository's Git objects", "unavailable in upstream history", "no recorded source commit"} {
		if !strings.Contains(reason, part) {
			t.Fatalf("reason %q lacks %q", reason, part)
		}
	}
	if body := string(h.read(".agents/skills/manual/SKILL.md")); !strings.Contains(body, "upstream v2; generic browser") {
		t.Fatalf("fallback did not install the new original:\n%s", body)
	}
	// The replacement registers a reachable tree, so the next update merges.
	h.write(".agents/skills/manual/SKILL.md", manifest("canonical-manual", "upstream v2; default browser"))
	h.commitAll()
	h.writeOrigin("skills/manual/notes.md", "upstream v3 notes\n")
	h.originGit("add", "-A", "--", "skills")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "original v3")
	update = onlyUpdate(t, h.run(0, "update", "manual"))
	if update["status"] != "merged" || update["base"] != "upstream-history" {
		t.Fatalf("update after the fallback did not merge: %v", update)
	}
	if body := string(h.read(".agents/skills/manual/SKILL.md")); !strings.Contains(body, "default browser") {
		t.Fatal("adaptation made after the fallback was lost")
	}
}

func TestUpdateResolvesBaseFromGitObjectsAndHistory(t *testing.T) {
	cases := []struct {
		name string
		// pristine commits the imported original before adapting it.
		pristine bool
		// rewrite removes the recorded source commit from the upstream history.
		rewrite bool
		retain  string
		tamper  bool
		base    string
	}{
		{name: "repository objects", pristine: true, base: "repository"},
		{name: "upstream history", base: "upstream-history"},
		{name: "ignored legacy snapshot", tamper: true, base: "upstream-history"},
		{name: "force-pushed upstream", rewrite: true},
		{name: "tree retained on another branch", rewrite: true, retain: "branch", base: "upstream-history"},
		{name: "tree retained by a tag", rewrite: true, retain: "tag", base: "upstream-history"},
		{name: "recorded commit outside advertised histories", rewrite: true, retain: "custom", base: "upstream"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.publishDemo(t, demoV1, nil)
			h.run(0, "add", "fixture/source:demo")
			if test.pristine {
				h.commitAll()
			}
			if test.tamper {
				h.write(originals+"/demo/SKILL.md", manifest("demo", "tampered base"))
			}
			h.adapt(t, "line two", "line two adapted locally")
			h.commitAll()
			if test.rewrite {
				branch := h.originGit("rev-parse", "--abbrev-ref", "HEAD")
				if test.retain == "custom" {
					h.originGit("update-ref", "refs/keep/original", "HEAD")
				} else if test.retain == "tag" {
					h.originGit("-c", "tag.gpgsign=false", "tag", "retained-original")
				} else if test.retain != "" {
					h.originGit(test.retain, "retained-original")
				}
				h.originGit("checkout", "-q", "--orphan", "rewritten")
				h.writeOrigin("skills/demo/SKILL.md", manifest("demo", "rewritten base"))
				h.originGit("add", "-A", "--", "skills")
				h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "rewritten history")
				h.originGit("branch", "-D", branch)
				h.originGit("branch", "-m", branch)
				h.originGit("reflog", "expire", "--expire=now", "--all")
				h.originGit("gc", "-q", "--prune=now")
			}
			h.publishDemo(t, strings.Replace(demoV1, "line five", "line five from upstream v2", 1), nil)
			update := onlyUpdate(t, h.run(0, "update", "demo"))
			body := string(h.read(".agents/skills/demo/SKILL.md"))
			if test.tamper && string(h.read(originals+"/demo/SKILL.md")) != manifest("demo", "tampered base") {
				t.Fatal("update modified a legacy snapshot")
			}
			if test.base == "" {
				if update["status"] != "replaced" || !strings.Contains(str(update["reason"]), "unavailable upstream") {
					t.Fatalf("lost source commit was not reported: %v", update)
				}
				if strings.Contains(body, "adapted locally") {
					t.Fatal("replacement kept content it could not merge")
				}
				return
			}
			if update["status"] != "merged" || update["base"] != test.base {
				t.Fatalf("base: got %v want %s", update, test.base)
			}
			if !strings.Contains(body, "line two adapted locally") || !strings.Contains(body, "line five from upstream v2") {
				t.Fatalf("merge lost content:\n%s", body)
			}
		})
	}
}

func TestUpdateReportsRefreshedMergedInputs(t *testing.T) {
	h := newHarness(t)
	h.run(0, "merge", "--name", "combined", "fixture/source:new-skill", "fixture/source:manual")
	h.commitAll()
	h.upstreamSkill("new-skill", "canonical-new-skill", "upstream v2")
	h.originGit("add", "-A", "--", "skills")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "new-skill v2")
	update := onlyUpdate(t, h.run(0, "update", "combined"))
	if update["layout"] != "merged" || update["status"] != "refreshed" {
		t.Fatalf("merged update report: %v", update)
	}
	changed := map[string]bool{}
	for _, raw := range update["inputs"].([]any) {
		input := raw.(map[string]any)
		changed[str(input["path"])] = input["changed"].(bool)
	}
	if !changed[".agents/skills/combined/references/new-skill"] || changed[".agents/skills/combined/references/manual"] {
		t.Fatalf("refreshed inputs: %v", changed)
	}
	if _, err := os.Stat(filepath.Join(h.root, originals, "combined")); !os.IsNotExist(err) {
		t.Fatal("a merged skill kept a separate base snapshot")
	}
}

func TestCommandAdapterUpdatesMergeFromUpstreamHistory(t *testing.T) {
	for _, backend := range []string{"skills", "gh"} {
		t.Run(backend, func(t *testing.T) {
			h := newHarness(t)
			served := filepath.Join(h.base, "served")
			serve := func(body string) {
				t.Helper()
				content := manifest("new-skill", body)
				h.writeOrigin("skills/new-skill/SKILL.md", content)
				h.originGit("add", "--", "skills/new-skill")
				h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "served original")
				h.writeFile(filepath.Join(served, "SKILL.md"), content)
				h.writeFile(filepath.Join(h.base, "served-lock.json"), mustJSON(map[string]any{"version": 1, "skills": map[string]any{"new-skill": map[string]any{
					"source": "fixture/source", "sourceType": "github", "skillPath": "skills/new-skill/SKILL.md",
					"computedHash": fmt.Sprintf("%x", sha256.Sum256([]byte("SKILL.md"+content))),
				}}}))
			}
			lock := "skills-lock.json"
			if backend == "gh" {
				lock = "\"$HOME/.agents/.skill-lock.json\""
			}
			script := "#!/bin/sh\nset -eu\n[ \"$1\" != auth ] || exit 1\n" +
				"if [ \"$1\" = --version ]; then printf '1.7.0\\n'; exit 0; fi\n" +
				"mkdir -p .agents/skills/new-skill \"$HOME/.agents\"\ncp '" + served + "/SKILL.md' .agents/skills/new-skill/SKILL.md\n" +
				"cp '" + h.base + "/served-lock.json' " + lock + "\n"
			h.writeFile(filepath.Join(h.binDir, backend), script)
			if err := os.Chmod(filepath.Join(h.binDir, backend), 0o755); err != nil {
				t.Fatal(err)
			}
			serve(demoV1)
			h.run(0, "--adapter", backend, "add", "fixture/source:new-skill")
			if _, ok := h.upstreamSkills()["new-skill"].(map[string]any)["sourceCommit"]; ok {
				t.Fatal("command adapters do not report a source commit")
			}
			body := string(h.read(".agents/skills/new-skill/SKILL.md"))
			h.write(".agents/skills/new-skill/SKILL.md", strings.Replace(body, "line two", "line two adapted locally", 1))
			h.write(".agents/skills/new-skill/scripts/am.test.mjs", "local test\n")
			h.commitAll()

			serve(strings.Replace(demoV1, "line five", "line five from upstream v2", 1))
			update := onlyUpdate(t, h.run(0, "--adapter", backend, "update", "new-skill"))
			if update["status"] != "merged" || update["base"] != "upstream-history" {
				t.Fatalf("command adapter update did not merge from upstream history: %v", update)
			}
			if string(h.read(".agents/skills/new-skill/scripts/am.test.mjs")) != "local test\n" {
				t.Fatal("command adapter merge removed local-only files")
			}
			body = string(h.read(".agents/skills/new-skill/SKILL.md"))
			if !strings.Contains(body, "line two adapted locally") || !strings.Contains(body, "line five from upstream v2") {
				t.Fatalf("merge lost content:\n%s", body)
			}
		})
	}
}

func TestUpdateReportsBinaryAndDeletionConflictsKeepingLocalFiles(t *testing.T) {
	h := newHarness(t)
	h.publishDemo(t, demoV1, map[string]string{"asset.bin": "\x00base", "drop.md": "drop v1\n", "keep.md": "keep v1\n"})
	h.run(0, "add", "fixture/source:demo")
	h.write(".agents/skills/demo/asset.bin", "\x00local")
	h.write(".agents/skills/demo/drop.md", "drop adapted locally\n")
	if err := os.Remove(filepath.Join(h.root, ".agents/skills/demo/keep.md")); err != nil {
		t.Fatal(err)
	}
	h.commitAll()

	h.writeOrigin("skills/demo/asset.bin", "\x00upstream")
	h.writeOrigin("skills/demo/keep.md", "keep v2\n")
	h.originGit("rm", "-q", "--", "skills/demo/drop.md")
	h.originGit("add", "-A", "--", "skills")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "demo v2")
	update := onlyUpdate(t, h.run(0, "update", "demo"))

	if update["status"] != "conflicted" {
		t.Fatalf("non-text conflicts were not reported: %v", update)
	}
	kinds := map[string]string{}
	for _, raw := range update["conflicts"].([]any) {
		conflict := raw.(map[string]any)
		if conflict["lines"] != nil {
			t.Fatalf("a non-text conflict reported line ranges: %v", conflict)
		}
		kinds[str(conflict["path"])] = str(conflict["kind"])
	}
	want := map[string]string{"asset.bin": "binary", "drop.md": "modified locally, deleted upstream", "keep.md": "deleted locally, modified upstream"}
	if len(kinds) != len(want) {
		t.Fatalf("conflicts: got %v want %v", kinds, want)
	}
	for path, kind := range want {
		if kinds[path] != kind {
			t.Fatalf("conflicts: got %v want %v", kinds, want)
		}
	}
	if string(h.read(".agents/skills/demo/asset.bin")) != "\x00local" || string(h.read(".agents/skills/demo/drop.md")) != "drop adapted locally\n" {
		t.Fatal("a non-text conflict did not keep the local file")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/demo/keep.md")); !os.IsNotExist(err) {
		t.Fatal("a locally deleted file was restored")
	}

}

func TestUpdateIgnoresMarkerShapedTextInCleanMerges(t *testing.T) {
	h := newHarness(t)
	h.publishDemo(t, demoV1, nil)
	h.run(0, "add", "fixture/source:demo")
	h.adapt(t, "line one", "line one adapted locally")
	h.commitAll()

	// Documentation about conflicts may quote a complete diff3 block verbatim.
	example := "line five\n\n<<<<<<< local\nmine\n||||||| previous original\nbase\n=======\ntheirs\n>>>>>>> new original"
	h.publishDemo(t, strings.Replace(demoV1, "line five", example, 1), nil)
	update := onlyUpdate(t, h.run(0, "update", "demo"))

	if update["status"] != "merged" || update["conflicts"] != nil {
		t.Fatalf("quoted markers were reported as a conflict: %v", update)
	}
	body := string(h.read(".agents/skills/demo/SKILL.md"))
	if !strings.Contains(body, "line one adapted locally") || !strings.Contains(body, ">>>>>>> new original") {
		t.Fatalf("merge lost content:\n%s", body)
	}
}

func TestLegacyBaseSnapshotsAreLeftUntouched(t *testing.T) {
	h := newHarness(t)
	h.publishDemo(t, demoV1, nil)
	legacy := originals + "/demo/SKILL.md"
	h.write(legacy, "legacy user-edited copy\n")
	h.commitAll()
	check := func() {
		t.Helper()
		if string(h.read(legacy)) != "legacy user-edited copy\n" {
			t.Fatal("command changed a legacy base snapshot")
		}
	}
	h.run(0, "add", "fixture/source:demo")
	check()
	h.adapt(t, "line two", "local adaptation")
	h.commitAll()
	h.publishDemo(t, strings.Replace(demoV1, "line five", "upstream change", 1), nil)
	h.run(0, "update", "demo")
	check()
	h.commitAll()
	h.run(0, "remove", "demo")
	check()
	h.commitAll()
	h.run(0, "merge", "--name", "demo", "fixture/source:new-skill")
	check()
}

func TestUpdateDoesNotReplaceSkillsWhenHistoryFetchFails(t *testing.T) {
	h := newHarness(t)
	h.publishDemo(t, demoV1, nil)
	h.run(0, "add", "fixture/source:demo")
	registrations := h.upstreamSkills()
	delete(registrations["demo"].(map[string]any), "sourceCommit")
	h.write("skills-lock.json", mustJSON(map[string]any{"version": 3, "skills": registrations}))
	h.adapt(t, "line two", "local adaptation")
	h.write(".agents/skills/demo/scripts/am.test.mjs", "local test\n")
	h.commitAll()
	before := h.git("status", "--porcelain")
	h.publishDemo(t, strings.Replace(demoV1, "line five", "upstream change", 1), nil)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in *--filter=blob:none*) exit 44 ;; esac\nexec '" + realGit + "' \"$@\"\n"
	h.writeFile(filepath.Join(h.binDir, "git"), script)
	if err := os.Chmod(filepath.Join(h.binDir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := h.try("update", "demo")
	if code != 1 || !strings.Contains(stderr, "upstream history fetch failed") {
		t.Fatalf("history failure: exit %d %s", code, stderr)
	}
	if h.git("status", "--porcelain") != before || string(h.read(".agents/skills/demo/scripts/am.test.mjs")) != "local test\n" {
		t.Fatal("failed history fetch changed the repository")
	}
}
