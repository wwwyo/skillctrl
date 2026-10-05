package upstream

import "testing"

func TestProjectContentHashMatchesSkillsCLIOrdering(t *testing.T) {
	contents := map[string][]byte{
		"z.md": []byte("z"), "a.md": []byte("a"), "A.md": []byte("A"),
		"_note.md": []byte("_"), "-note.md": []byte("-"), "SKILL.md": []byte("body\n"),
		"é.md": []byte("accent"), "日本.md": []byte("jp"),
	}
	files := map[string]treeFile{}
	for name := range contents {
		files[name] = treeFile{oid: name}
	}
	// Generated independently with Node's localeCompare and crypto.createHash,
	// using the path-and-content algorithm in the skills CLI's local-lock.ts.
	const expected = "813efb2580ce0717ebcce59be94db403ed8167639e9d8a6c302a097f285cbb7d"
	if got := contentHash(files, contents); got != expected {
		t.Fatalf("native project hash differs: %s", got)
	}
	files["node_modules/package.js"] = treeFile{oid: "ignored"}
	contents["ignored"] = []byte("ignored package")
	if got := contentHash(files, contents); got != expected {
		t.Fatal("native project hash included node_modules")
	}
}
