package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wwwyo/skillctrl/internal/skillstate"
)

func privateState(t *testing.T, h *harness) *skillstate.Record {
	t.Helper()
	value, err := skillstate.Parse(h.read(skillstate.Lock))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func legacyState(t *testing.T, h *harness) {
	t.Helper()
	h.run(0, "add", "fixture/source:new-skill", "--name", "local-name")
	value := privateState(t, h)
	h.write(skillstate.LegacyUpstreams, mustJSON(map[string]any{"version": 1, "skills": value.Upstreams, "future": map[string]any{"preserve": true}}))
	h.write(skillstate.LegacyAccepted, mustJSON(map[string]any{"version": 2, "skills": value.AcceptedHashes}))
	if err := os.Remove(filepath.Join(h.root, skillstate.Lock)); err != nil {
		t.Fatal(err)
	}
	h.commitAll()
}

// TestUnifiedLockMigration exercises both mutating entry points through the CLI.
func TestUnifiedLockMigration(t *testing.T) {
	for _, command := range []string{"record", "add"} {
		t.Run(command, func(t *testing.T) {
			h := newHarness(t)
			legacyState(t, h)
			native := h.read("skills-lock.json")
			accepted := h.acceptedBytes()
			upstreams := h.read(skillstate.LegacyUpstreams)
			legacyHashes := h.read(skillstate.LegacyAccepted)
			h.write(".agents/skills/manual/SKILL.md", manifest("manual", "unverified local edit"))
			h.git("add", "--", ".agents/skills/manual/SKILL.md")
			index := h.read(".fixture-git/index")
			before := h.git("status", "--porcelain")
			h.run(0, "check")
			if command == "record" {
				h.run(0, "--dry-run", "record")
			} else {
				h.run(0, "--dry-run", "add", "fixture/source:new-skill", "--name", "second-name")
			}
			if before != h.git("status", "--porcelain") {
				t.Fatal("read-only command migrated legacy state")
			}
			h.run(1, "add", "fixture/source:linked")
			if !bytes.Equal(upstreams, h.read(skillstate.LegacyUpstreams)) || !bytes.Equal(legacyHashes, h.read(skillstate.LegacyAccepted)) || before != h.git("status", "--porcelain") {
				t.Fatal("failed import changed legacy state")
			}
			if command == "record" {
				h.run(0, "record")
			} else {
				h.run(0, "add", "fixture/source:new-skill", "--name", "second-name")
			}
			value := privateState(t, h)
			if !bytes.Equal(accepted, h.acceptedBytes()) || !bytes.Equal(index, h.read(".fixture-git/index")) || !bytes.Equal(native, h.read("skills-lock.json")) {
				t.Fatal("migration advanced acceptance, changed native registrations, or disturbed staging")
			}
			if value.Upstreams["local-name"].(map[string]any)["skill"] != "new-skill" {
				t.Fatal("migration lost alias identity")
			}
			var extra map[string]any
			if err := json.Unmarshal(value.Extra["future"], &extra); err != nil || extra["preserve"] != true {
				t.Fatal("migration discarded unknown metadata")
			}
			for _, path := range []string{skillstate.LegacyUpstreams, skillstate.LegacyAccepted} {
				if _, err := os.Stat(filepath.Join(h.root, path)); !os.IsNotExist(err) {
					t.Fatalf("legacy file remains: %s", path)
				}
			}
			if h.run(0, "check")["local"].(map[string]any)["needs_review"] != true {
				t.Fatal("migration accepted an unverified edit")
			}
			sources := mustJSON(value.Upstreams)
			h.run(0, "record", "manual")
			if mustJSON(privateState(t, h).Upstreams) != sources {
				t.Fatal("record changed acquisition metadata")
			}
			if h.run(0, "check")["local"].(map[string]any)["needs_review"] != false {
				t.Fatal("explicit record did not accept the named edit")
			}
		})
	}
}

func TestUnifiedLockFailureAndPrecedence(t *testing.T) {
	t.Run("malformed legacy acceptance blocks acquisition before import", func(t *testing.T) {
		h := newHarness(t)
		legacyState(t, h)
		h.write(skillstate.LegacyAccepted, "invalid json")
		before := h.git("status", "--porcelain")
		h.run(1, "add", "fixture/source:new-skill", "--name", "second-name")
		if before != h.git("status", "--porcelain") {
			t.Fatal("invalid legacy lock allowed partial import")
		}
	})
	t.Run("canonical state wins over stale legacy registrations", func(t *testing.T) {
		h := newHarness(t)
		h.write(skillstate.LegacyUpstreams, `{"version":1,"skills":{"stale":{"source":"fixture/source","sourceType":"github"}}}`)
		h.write(skillstate.LegacyAccepted, `{"version":2,"skills":{"manual":"stale-hash"}}`)
		accepted := h.acceptedBytes()
		h.run(0, "check")
		if _, exists := h.upstreamSkills()["stale"]; exists {
			t.Fatal("legacy tracking resurrected a registration")
		}
		h.run(0, "record")
		if !bytes.Equal(accepted, h.acceptedBytes()) {
			t.Fatal("stale legacy acceptance replaced canonical hashes")
		}
	})
}

// TestAcquisitionPreservesLaterAcceptance prevents a prepared import from rolling
// back a record made while its acquisition executable was still running.
func TestAcquisitionPreservesLaterAcceptance(t *testing.T) {
	h := newHarness(t)
	h.write(".agents/skills/manual/SKILL.md", manifest("manual", "verified local edit"))
	body := manifest("new-skill", "downloaded original")
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("SKILL.md"+body)))
	metadata := mustJSON(map[string]any{"version": 1, "skills": map[string]any{"new-skill": map[string]any{
		"source": "fixture/source", "sourceType": "github", "skillPath": "skills/new-skill/SKILL.md", "computedHash": digest,
	}}})
	script := "#!/bin/sh\nset -eu\nif [ \"$1\" = --version ]; then printf '1.7.0\\n'; exit 0; fi\n" +
		fmt.Sprintf("(cd '%s' && SKILLCTRL_ADAPTER=git '%s' record manual > '%s/record.json')\n", h.root, h.binary, h.base) +
		"mkdir -p .agents/skills/new-skill\ncat > .agents/skills/new-skill/SKILL.md <<'BODY'\n" + body + "BODY\n" +
		"cat > skills-lock.json <<'LOCK'\n" + metadata + "\nLOCK\n"
	h.writeFile(filepath.Join(h.binDir, "skills"), script)
	if err := os.Chmod(filepath.Join(h.binDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := h.lockedSkills()["manual"]
	h.run(0, "--adapter", "skills", "add", "fixture/source:new-skill")
	if h.lockedSkills()["manual"] == before {
		t.Fatal("import rolled back acceptance recorded during acquisition")
	}
	if h.run(0, "check")["local"].(map[string]any)["needs_review"] != false {
		t.Fatal("recorded edit still reports drift")
	}
}
