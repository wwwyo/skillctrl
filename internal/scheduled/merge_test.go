package scheduled_test

import (
	"encoding/json"
	"testing"

	"github.com/wwwyo/skillctrl/internal/scheduled"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

func TestScheduledMergedInputsVerifyEachOriginalAndKeepOutputUntouched(t *testing.T) {
	for _, layout := range []string{"legacy", "references"} {
		for _, scenario := range []string{"valid second-source update", "changed identity", "forged original hash", "extra snapshot", "changed merged output", "removed array", "intent-free update"} {
			t.Run(layout+"/"+scenario, func(t *testing.T) {
				f := newFixture(t)
				prefix := ".agents/skills/manual/" + upstream.LegacySourceDirectory + "/"
				children := []string{"0", "1"}
				if layout == "references" {
					prefix = ".agents/skills/manual/references/"
					children = []string{"first", "second"}
				}
				for _, child := range children {
					f.write(prefix+child+"/SKILL.md", "Original input v1\n")
				}
				f.write(".agents/skills/manual/references/guide.md", "Handwritten reference.\n")
				f.git("add", "-A")
				tree := f.git("write-tree")
				inputs := []map[string]any{
					{"source": "fixture/first", "sourceType": "github", "skill": "first", "skillFolderHash": f.git("rev-parse", tree+":"+prefix+children[0])},
					{"source": "fixture/second", "sourceType": "github", "skill": "second", "skillFolderHash": f.git("rev-parse", tree+":"+prefix+children[1])},
				}
				writeRecord := func(entry map[string]any) {
					t.Helper()
					if layout == "references" && entry["sources"] != nil {
						entry["sourceLayout"] = "references"
					}
					data, err := json.Marshal(map[string]any{"version": 3, "skills": map[string]any{"manual": entry}})
					if err != nil {
						t.Fatal(err)
					}
					f.write(upstream.Lock, string(data))
				}
				writeRecord(map[string]any{"sources": inputs})
				if scenario == "intent-free update" {
					f.git("rm", "--", ".agents/skillctrl/intents/manual.md")
				}
				f.git("add", "-A")
				f.git("commit", "-qm", "merged baseline")
				base := f.git("rev-parse", "HEAD")
				f.write(prefix+children[1]+"/SKILL.md", "Original input v2\n")
				f.git("add", "-A")
				tree = f.git("write-tree")
				inputs[1]["skillFolderHash"] = f.git("rev-parse", tree+":"+prefix+children[1])
				switch scenario {
				case "changed identity":
					inputs[1]["source"] = "fixture/other"
				case "forged original hash":
					inputs[1]["skillFolderHash"] = inputs[0]["skillFolderHash"]
				case "extra snapshot":
					f.write(prefix+"2/SKILL.md", "Unexpected input\n")
				case "changed merged output":
					f.write(".agents/skills/manual/SKILL.md", "Unreviewed output replacement\n")
				}
				writeRecord(map[string]any{"sources": inputs})
				if scenario == "removed array" {
					writeRecord(map[string]any{"source": "", "sourceType": "github", "skillFolderHash": f.git("rev-parse", tree+":.agents/skills/manual")})
				}
				f.git("add", "-A")
				err := scheduled.ValidateImport(f.dir, base, f.git("write-tree"))
				if (scenario == "valid second-source update" || scenario == "intent-free update") && err != nil {
					t.Fatalf("valid source update was rejected: %v", err)
				}
				if scenario != "valid second-source update" && scenario != "intent-free update" && err == nil {
					t.Fatal("invalid merged inputs were accepted")
				}
			})
		}
	}
}
