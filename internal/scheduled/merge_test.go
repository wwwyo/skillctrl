package scheduled_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wwwyo/skillctrl/internal/scheduled"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

func TestScheduledMergedInputsVerifyEachOriginalAndKeepOutputUntouched(t *testing.T) {
	for _, scenario := range []string{"valid second-source update", "changed identity", "forged original hash", "extra snapshot", "changed merged output", "removed array", "missing intent"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			prefix := ".agents/skills/manual/" + upstream.SourceDirectory + "/"
			for index := range 2 {
				f.write(fmt.Sprintf("%s%d/SKILL.md", prefix, index), "Original input v1\n")
			}
			f.git("add", "-A")
			tree := f.git("write-tree")
			inputs := []map[string]any{
				{"source": "fixture/first", "sourceType": "github", "skill": "first", "skillFolderHash": f.git("rev-parse", tree+":"+prefix+"0")},
				{"source": "fixture/second", "sourceType": "github", "skill": "second", "skillFolderHash": f.git("rev-parse", tree+":"+prefix+"1")},
			}
			writeRecord := func(entry map[string]any) {
				t.Helper()
				data, err := json.Marshal(map[string]any{"version": 3, "skills": map[string]any{"manual": entry}})
				if err != nil {
					t.Fatal(err)
				}
				f.write(upstream.Lock, string(data))
			}
			writeRecord(map[string]any{"sources": inputs})
			if scenario == "missing intent" {
				f.write(".agents/skillctrl/intents/manual.md", " \n")
			}
			f.git("add", "-A")
			f.git("commit", "-qm", "merged baseline")
			base := f.git("rev-parse", "HEAD")
			f.write(prefix+"1/SKILL.md", "Original input v2\n")
			f.git("add", "-A")
			tree = f.git("write-tree")
			inputs[1]["skillFolderHash"] = f.git("rev-parse", tree+":"+prefix+"1")
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
			if scenario == "valid second-source update" && err != nil {
				t.Fatalf("valid source update was rejected: %v", err)
			}
			if scenario != "valid second-source update" && err == nil {
				t.Fatal("invalid merged inputs were accepted")
			}
		})
	}
}
