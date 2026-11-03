package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// branchTarget is one branch of a workspace, with the workspace resolved to
// its ID.
type branchTarget struct {
	Organization string
	WorkspaceID  string
	Branch       string
}

// branchLocks serializes writes to a branch, because resources writing to it
// at once would share, and overwrite, the user's one draft on it.
var branchLocks sync.Map

func lockBranch(t branchTarget) func() {
	m, _ := branchLocks.LoadOrStore(t.WorkspaceID+"\x00"+t.Branch, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// errDraftConflict is a draft that the branch moved under, which a fresh
// draft at the new tip resolves.
var errDraftConflict = errors.New("the branch changed while Terraform wrote to it")

// errForeignDraft is a draft with changes that the user started elsewhere,
// such as in the web UI.
var errForeignDraft = errors.New("you have unpublished changes in a draft on this branch, " +
	"which committing would publish along with Terraform's change. Commit or discard the draft " +
	"in the web UI, or run Terraform with another user's key")

// fileWrite is one change to one file. A nil Content deletes the file.
type fileWrite struct {
	Path    string
	Content *string
	Message string
	// Create refuses to replace a file that exists.
	Create bool
}

// publishFile writes one file to a branch through a draft of its own, and
// returns the branch tip that holds the change. A write that changes nothing
// publishes nothing.
func publishFile(ctx context.Context, c *client.Client, t branchTarget, w fileWrite) (string, error) {
	account, err := c.ReadAccount(ctx)
	if err != nil {
		return "", err
	}
	if account.User == nil {
		return "", fmt.Errorf("configuration drafts belong to users, and the credential is a %s; use a user's API key", account.Tag)
	}
	scope := client.DraftScope{Organization: t.Organization, Workspace: t.WorkspaceID, UserID: account.User.ID, Branch: t.Branch}
	defer lockBranch(t)()
	commit, err := publishOnce(ctx, c, scope, w)
	if errors.Is(err, errDraftConflict) {
		commit, err = publishOnce(ctx, c, scope, w)
	}
	return commit, err
}

func publishOnce(ctx context.Context, c *client.Client, s client.DraftScope, w fileWrite) (string, error) {
	tip, current, err := readBranchFile(ctx, c, s.Organization, s.Workspace, s.Branch, w.Path)
	if client.IsNotFound(err) {
		return "", fmt.Errorf("branch %q does not exist", s.Branch)
	}
	if err != nil {
		return "", err
	}
	switch {
	case w.Content == nil && current == nil:
		return tip, nil
	case w.Content != nil && current != nil && *current == *w.Content:
		return tip, nil
	case w.Create && current != nil:
		return "", fmt.Errorf("%s already exists on branch %q with other content; import it to manage it with Terraform", w.Path, s.Branch)
	}
	draft, err := startDraft(ctx, c, s)
	if err != nil {
		return "", err
	}
	change := client.FileChange{Type: "put", Path: w.Path, Text: w.Content}
	if w.Content == nil {
		change.Type = "delete"
	}
	commit, err := commitChange(ctx, c, s, draft, w.Message, change)
	if err != nil {
		discardDraft(ctx, c, s)
	}
	return commit, err
}

// startDraft starts an empty draft at the branch tip. An empty draft that the
// web UI opened is replaced, but one with changes is refused, because
// committing it would publish them.
func startDraft(ctx context.Context, c *client.Client, s client.DraftScope) (*client.Draft, error) {
	existing, err := c.ReadDraft(ctx, s)
	switch {
	case client.IsNotFound(err):
	case err != nil:
		return nil, err
	default:
		empty, err := isEmptyDraft(ctx, c, s, existing)
		if err != nil {
			return nil, err
		}
		if !empty {
			return nil, errForeignDraft
		}
		if err := c.DiscardDraft(ctx, s); err != nil {
			return nil, err
		}
	}
	return c.CreateDraft(ctx, s)
}

// isEmptyDraft tells a draft that only an opened editor started, the way the
// platform does: no message, and no file changed against the revision it
// edits.
func isEmptyDraft(ctx context.Context, c *client.Client, s client.DraftScope, d *client.Draft) (bool, error) {
	if d.Message != nil || d.Status != "ready" {
		return false, nil
	}
	head, err := c.ReadCommit(ctx, s.Organization, s.Workspace, d.HeadSHA)
	if err != nil {
		return false, err
	}
	return len(head.Files) == 0, nil
}

func commitChange(ctx context.Context, c *client.Client, s client.DraftScope, draft *client.Draft, message string, change client.FileChange) (string, error) {
	saved, err := c.SaveDraft(ctx, s, draft.HeadSHA, &message, []client.FileChange{change})
	if isConflict(err) {
		return "", errDraftConflict
	}
	if err != nil {
		return "", err
	}
	if saved.Status != "ready" {
		return "", errDraftConflict
	}
	commit, err := c.CommitDraft(ctx, s)
	if isConflict(err) {
		return "", errDraftConflict
	}
	if err != nil {
		return "", err
	}
	if commit != saved.HeadSHA {
		return commit, fmt.Errorf("the platform published %s instead of Terraform's change %s: "+
			"another session saved into your draft on branch %q while Terraform wrote to it", commit, saved.HeadSHA, s.Branch)
	}
	return commit, nil
}

func isConflict(err error) bool {
	var apiErr *client.Error
	return errors.As(err, &apiErr) && (apiErr.Tag == client.TagDraftConflict || apiErr.Status == http.StatusConflict)
}

// discardDraft drops Terraform's draft after a failure, so that the user's
// draft list stays clean, even when the failure was a cancellation.
func discardDraft(ctx context.Context, c *client.Client, s client.DraftScope) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = c.DiscardDraft(ctx, s)
}

// readBranchFile reads the tip of a branch and the file at it; the content
// is nil when the file does not exist.
func readBranchFile(ctx context.Context, c *client.Client, org, workspace, branch, path string) (string, *string, error) {
	tree, err := c.ReadFileTree(ctx, org, workspace, branch)
	if err != nil {
		return "", nil, err
	}
	found := false
	for _, e := range tree.Entries {
		if e.Path == path {
			if e.Kind != "file" {
				return "", nil, fmt.Errorf("%s is a directory on branch %q", path, branch)
			}
			found = true
			break
		}
	}
	if !found {
		return tree.Commit, nil, nil
	}
	f, err := c.ReadFile(ctx, org, workspace, tree.Commit, path)
	if client.IsNotFound(err) {
		return tree.Commit, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if f.Text == nil {
		return "", nil, fmt.Errorf("%s on branch %q is binary or too large for a text file", path, branch)
	}
	return tree.Commit, f.Text, nil
}

// checkConfigurationPath refuses paths that the platform refuses to save, or
// that no deployment would take.
func checkConfigurationPath(p string) error {
	if p == "" {
		return errors.New("the path is empty")
	}
	if strings.ContainsAny(p, "\x00\r\n\\") {
		return fmt.Errorf("%q contains a backslash, a newline or a NUL character", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return fmt.Errorf("%q is not a relative path of the repository: it has an empty, \".\", \"..\" or \".git\" component", p)
		}
	}
	switch {
	case p == "package.yaml" || p == "package.lock" || strings.HasPrefix(p, "src/"):
		return fmt.Errorf("%s is part of a package, which deployments do not support yet", p)
	case strings.HasPrefix(p, "packages/") && !strings.Contains(strings.TrimPrefix(p, "packages/"), "/"):
		return fmt.Errorf("%s is not inside a package; use packages/<package>/…", p)
	case strings.HasPrefix(p, "pipelines/") && !strings.HasSuffix(p, ".tql"):
		return fmt.Errorf("%s is not a pipeline: files under pipelines/ end in .tql", p)
	}
	return nil
}
