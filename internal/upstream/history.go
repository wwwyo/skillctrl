package upstream

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
)

var errHistoryUnavailable = errors.New("upstream history base unavailable")

// historyOriginal resolves a registered tree independently of the acquisition
// adapter. Full history is necessary because native locks need not name a commit.
func historyOriginal(request ExportRequest) (map[string]file, error) {
	tree := field(request.Previous, "skillFolderHash")
	if !commitPattern.MatchString(tree) {
		return nil, fmt.Errorf("%w: registration has no valid original tree hash", errHistoryUnavailable)
	}
	identifier := field(request.Previous, "sourceUrl")
	if identifier == "" {
		identifier = field(request.Previous, "source")
	}
	source, err := Source(identifier)
	if err != nil {
		return nil, fmt.Errorf("registration has no supported upstream URL")
	}
	if !strings.EqualFold(source, request.Source) {
		return nil, fmt.Errorf("registered source URL changed source identity")
	}
	// Share one disposable fetch per source in this preparation, without storing
	// upstream history or copies of originals in the destination repository.
	directory := filepath.Join(request.Directory, fmt.Sprintf("history-%x", sha256.Sum256([]byte(strings.ToLower(source)))))
	if _, err := os.Stat(filepath.Join(directory, "skillctrl-history-ready")); os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, err
		}
		if err := gitx.SafeRun(directory, "init", "--bare", "--quiet", "--template="); err != nil {
			return nil, err
		}
		url := "https://github.com/" + source + ".git"
		for _, setting := range [][2]string{
			{"remote.origin.url", url},
			{"remote.origin.promisor", "true"},
			{"remote.origin.partialclonefilter", "blob:none"},
		} {
			if err := gitx.SafeRun(directory, "config", setting[0], setting[1]); err != nil {
				return nil, err
			}
		}
		env := removeEnv(gitx.Environment("GIT_TERMINAL_PROMPT=0"), "OPENCODE_API_KEY")
		// No depth limit: the registered tree may be arbitrarily old or reachable
		// only through a non-default branch or tag. Blobs are fetched on demand.
		if err := gitx.SafeEnv(directory, env, "fetch", "--quiet", "--filter=blob:none", "--no-tags", "origin",
			"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
			return nil, fmt.Errorf("upstream history fetch failed")
		}
		if err := os.WriteFile(filepath.Join(directory, "skillctrl-history-ready"), nil, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	// Object reads stay local; only the explicit fetches above and below hit the network.
	offline := removeEnv(gitx.Environment("GIT_NO_LAZY_FETCH=1"), "OPENCODE_API_KEY")
	if err := gitx.SafeEnv(directory, offline, "cat-file", "-e", tree+"^{tree}"); err != nil {
		return nil, fmt.Errorf("%w: registered original tree %s is unavailable in upstream history", errHistoryUnavailable, shortHash(tree))
	}
	// cat-file lazily fetches one missing blob at a time. Batch the missing
	// objects of this tree first to avoid a network round trip per skill file.
	missing, err := gitx.SafeStdinEnv(directory, offline, nil, "rev-list", "--objects", "--missing=print", tree)
	if err != nil {
		return nil, fmt.Errorf("cannot list registered original tree")
	}
	var wants strings.Builder
	for _, line := range strings.Split(string(missing), "\n") {
		if oid, missing := strings.CutPrefix(line, "?"); missing {
			wants.WriteString(oid + "\n")
		}
	}
	if wants.Len() > 0 {
		env := removeEnv(gitx.Environment("GIT_TERMINAL_PROMPT=0"), "OPENCODE_API_KEY")
		// Like Git's own promisor fetch, skip negotiation: advertising the fetched
		// commits as haves lets GitHub omit blobs that are still reachable from them.
		if _, err := gitx.SafeStdinEnv(directory, env, []byte(wants.String()), "-c", "fetch.negotiationAlgorithm=noop",
			"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--filter=blob:none", "--stdin", "origin"); err != nil {
			return nil, fmt.Errorf("cannot fetch registered original blobs from upstream history")
		}
	}
	files, err := objectTree(directory, tree)
	if err != nil {
		return nil, fmt.Errorf("cannot read registered original from upstream history: %w", err)
	}
	if treeHash(files) != tree {
		return nil, fmt.Errorf("upstream history original does not match the registered tree hash")
	}
	return files, nil
}
