package adapt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
)

// GH abstracts the GitHub CLI so the publication logic can be exercised without
// network access. Every method maps to one gh invocation.
type GH interface {
	JSON(args []string, input []byte) ([]byte, error)
	Run(args []string, input []byte) ([]byte, error)
	SetupGit() error
}

// CLI is the real gh-backed implementation.
type CLI struct{}

// JSON runs gh and returns parsed JSON output.
func (CLI) JSON(args []string, input []byte) ([]byte, error) {
	out, err := CLI{}.Run(args, input)
	return out, err
}

// Run runs gh with the given arguments.
func (CLI) Run(args []string, input []byte) ([]byte, error) {
	command := exec.Command("gh", args...)
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("gh %s failed: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// SetupGit points Git at the gh credential helper for the push that follows.
func (CLI) SetupGit() error {
	command := exec.Command("gh", "auth", "setup-git")
	if err := command.Run(); err != nil {
		return fmt.Errorf("gh auth setup-git failed")
	}
	return nil
}

// PublishOptions carries the environment a publication run depends on.
type PublishOptions struct {
	Dir       string
	Plan      lock.Plan
	Directory string
	Repo      string
	Number    string
	GH        GH
}

// Publish pushes one normal child commit to the still-current triggering pull
// request and reports it. The run is refused if the pull request closed, moved,
// came from a fork, or targets its own base branch, because pushing to a
// different ref than the one that was reviewed would publish unreviewed content.
func Publish(options PublishOptions) (bool, error) {
	if err := ValidateHead(options.Dir, options.Plan); err != nil {
		return false, err
	}
	if options.Repo == "" || options.Number == "" {
		return false, fmt.Errorf("publish requires a repository and pull request number")
	}
	body, err := options.GH.JSON([]string{"api", fmt.Sprintf("repos/%s/pulls/%s", options.Repo, options.Number)}, nil)
	if err != nil {
		return false, err
	}
	var pull struct {
		State string `json:"state"`
		Head  struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := json.Unmarshal(body, &pull); err != nil {
		return false, fmt.Errorf("unexpected pull request response")
	}
	branch := pull.Head.Ref
	if pull.State != "open" || pull.Head.SHA != options.Plan.Head ||
		pull.Head.Repo.FullName != options.Repo || branch == pull.Base.Ref {
		return false, fmt.Errorf("PR closed, forked, or changed since selection; rerun on its new head")
	}
	if err := gitx.Run(options.Dir, "check-ref-format", "refs/heads/"+branch); err != nil {
		return false, err
	}
	report, err := ReadReport(options.Directory)
	if err != nil {
		return false, err
	}
	staged, err := gitx.Output(options.Dir, "diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	changed := strings.TrimSpace(string(staged)) != ""
	if changed {
		if err := Commit(options.Dir, "fix(skills): preserve saved intent after skill changes"); err != nil {
			return false, err
		}
		if err := options.GH.SetupGit(); err != nil {
			return false, err
		}
		if err := gitx.Run(options.Dir, "push", "origin", "HEAD:refs/heads/"+branch); err != nil {
			return false, err
		}
	}
	head, err := gitx.Output(options.Dir, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	text := fmt.Sprintf("Skill intent review (%s)\n\n%s", options.Plan.Head[:12], report)
	if changed {
		text += fmt.Sprintf("\n\nRecorded in `%s`. A push with GITHUB_TOKEN does not restart CI;"+
			" this commit is not the verification result for the pre-fix checks.", gitx.Trimmed(head))
	}
	comments, err := options.GH.JSON([]string{"api",
		fmt.Sprintf("repos/%s/issues/%s/comments?per_page=100", options.Repo, options.Number)}, nil)
	if err != nil {
		return false, err
	}
	var existing []struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(comments, &existing)
	for _, comment := range existing {
		if comment.Body == text {
			// A rerun of the same review must not post a duplicate comment.
			return changed, nil
		}
	}
	payload, err := json.Marshal(map[string]string{"body": text})
	if err != nil {
		return changed, err
	}
	if _, err := options.GH.JSON([]string{"api",
		fmt.Sprintf("repos/%s/issues/%s/comments", options.Repo, options.Number),
	}, payload); err != nil {
		return changed, err
	}
	return changed, nil
}

// Credential reports whether an inference credential is configured.
func Credential() bool { return os.Getenv("OPENCODE_API_KEY") != "" }
