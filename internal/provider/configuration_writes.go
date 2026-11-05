package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// branchTarget is one branch of a workspace, with the workspace resolved to
// its ID.
type branchTarget struct {
	Organization string
	WorkspaceID  string
	Branch       string
}

// branchLocks serializes writes to a branch within one Terraform run, so that
// they do not use up their commit attempts on each other's commits.
var branchLocks sync.Map

func lockBranch(t branchTarget) func() {
	m, _ := branchLocks.LoadOrStore(t.WorkspaceID+"\x00"+t.Branch, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// commitAttempts bounds the commits that a write tries while the branch
// moves under it, such as when another Terraform run writes to it.
const commitAttempts = 3

// fileWrite is one change to one file. A nil Content deletes the file.
type fileWrite struct {
	Path    string
	Content *string
	Message string
	// Create refuses to replace a file that exists.
	Create bool
}

// publishFile commits one change to a branch, against the tip that it read
// the file at, and returns the commit that holds the change. A write that
// changes nothing commits nothing.
func publishFile(ctx context.Context, c *client.Client, t branchTarget, w fileWrite) (string, error) {
	defer lockBranch(t)()
	tip, current, err := readBranchFile(ctx, c, t.Organization, t.WorkspaceID, t.Branch, w.Path)
	for attempt := 1; ; attempt++ {
		if client.IsNotFound(err) {
			return "", fmt.Errorf("branch %q does not exist", t.Branch)
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
			return "", fmt.Errorf("%s already exists on branch %q with other content; import it to manage it with Terraform", w.Path, t.Branch)
		}
		change := client.FileChange{Type: "put", Path: w.Path, Text: w.Content}
		if w.Content == nil {
			change.Type = "delete"
		}
		var commit string
		commit, err = c.CommitChanges(ctx, t.Organization, t.WorkspaceID, t.Branch, tip, w.Message, []client.FileChange{change})
		head, moved := client.MovedHead(err)
		switch {
		case client.IsNotFound(err):
			return "", fmt.Errorf("branch %q does not exist, or the credential may not commit to it: %w", t.Branch, err)
		case !moved:
			return commit, err
		case attempt == commitAttempts:
			return "", fmt.Errorf("branch %q moved during each of %d attempts to commit: %w", t.Branch, commitAttempts, err)
		}
		tip = head
		current, err = readFileAt(ctx, c, t.Organization, t.WorkspaceID, head, w.Path)
	}
}

// readFileAt reads a file at a commit; the content is nil when the file does
// not exist.
func readFileAt(ctx context.Context, c *client.Client, org, workspace, commit, path string) (*string, error) {
	f, err := c.ReadFile(ctx, org, workspace, commit, path)
	if client.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if f.Text == nil {
		return nil, fmt.Errorf("%s at commit %s is binary or too large for a text file", path, commit)
	}
	return f.Text, nil
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
	content, err := readFileAt(ctx, c, org, workspace, tree.Commit, path)
	if err != nil {
		return "", nil, err
	}
	return tree.Commit, content, nil
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
