package client

import (
	"context"
	"net/url"
)

var (
	opReadFileTree              = op("GET", "/organizations/{org}/workspaces/{workspace}/file-tree")
	opReadFile                  = op("GET", "/organizations/{org}/workspaces/{workspace}/file")
	opReadCommit                = op("GET", "/organizations/{org}/workspaces/{workspace}/commits/detail")
	opReadConfiguredPipelines   = op("GET", "/organizations/{org}/workspaces/{workspace}/pipelines")
	opCreateConfigurationDraft  = op("POST", "/organizations/{org}/workspaces/{workspace}/configuration/drafts/{userId}/")
	opReadConfigurationDraft    = op("GET", "/organizations/{org}/workspaces/{workspace}/configuration/drafts/{userId}/detail")
	opSaveConfigurationDraft    = op("POST", "/organizations/{org}/workspaces/{workspace}/configuration/drafts/{userId}/save")
	opCommitConfigurationDraft  = op("POST", "/organizations/{org}/workspaces/{workspace}/configuration/drafts/{userId}/commit")
	opDiscardConfigurationDraft = op("POST", "/organizations/{org}/workspaces/{workspace}/configuration/drafts/{userId}/discard")
)

// TagDraftConflict is the error tag for a draft whose branch moved, or whose
// change conflicts with it.
const TagDraftConflict = "DraftConflict"

// GitTree is every file and directory of a branch, resolved to one commit.
type GitTree struct {
	Commit  string         `json:"commit"`
	Entries []GitTreeEntry `json:"entries"`
}

// GitTreeEntry is one path of a tree; Kind is "file" or "directory".
type GitTreeEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}

// GitFile is one file at a commit. Text is absent for a binary or large file.
type GitFile struct {
	Path string  `json:"path"`
	Size int64   `json:"size"`
	Text *string `json:"text,omitempty"`
}

// GitCommitDetails is a commit and the files it changed against its first
// parent.
type GitCommitDetails struct {
	SHA     string   `json:"sha"`
	Message string   `json:"message"`
	Author  string   `json:"author"`
	Parents []string `json:"parents"`
	Files   []struct {
		Path string `json:"path"`
	} `json:"files"`
}

// ConfiguredPipelines is what a branch declares at its tip. Problem says why
// no deployment would take it.
type ConfiguredPipelines struct {
	Commit  *string `json:"commit,omitempty"`
	Problem *string `json:"problem,omitempty"`
}

// Draft is a user's unpublished change to a branch. Status is "ready" or
// "conflicting".
type Draft struct {
	WorkspaceID string  `json:"workspaceId"`
	OwnerID     string  `json:"ownerId"`
	Branch      string  `json:"branch"`
	HeadSHA     string  `json:"headSha"`
	Message     *string `json:"message,omitempty"`
	Status      string  `json:"status"`
}

// FileChange puts a text file (Type "put") or deletes one (Type "delete").
type FileChange struct {
	Type string  `json:"type"`
	Path string  `json:"path"`
	Text *string `json:"text,omitempty"`
}

// DraftScope names whose draft on which branch of which workspace.
type DraftScope struct {
	Organization string
	Workspace    string
	UserID       string
	Branch       string
}

func (s DraftScope) args() []string {
	return []string{s.Organization, s.Workspace, s.UserID}
}

func (s DraftScope) branch() any {
	return struct {
		Branch string `json:"branch"`
	}{s.Branch}
}

// ReadFileTree reads the tree at the tip of a branch.
func (c *Client) ReadFileTree(ctx context.Context, org, workspace, branch string) (*GitTree, error) {
	var out GitTree
	if err := c.call(ctx, opReadFileTree, []string{org, workspace}, url.Values{"branch": {branch}}, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadFile reads one file at a commit.
func (c *Client) ReadFile(ctx context.Context, org, workspace, commit, path string) (*GitFile, error) {
	var out GitFile
	query := url.Values{"commit": {commit}, "path": {path}}
	if err := c.call(ctx, opReadFile, []string{org, workspace}, query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadCommit reads a commit, including a draft's.
func (c *Client) ReadCommit(ctx context.Context, org, workspace, commit string) (*GitCommitDetails, error) {
	var out GitCommitDetails
	if err := c.call(ctx, opReadCommit, []string{org, workspace}, url.Values{"commit": {commit}}, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadConfiguredPipelines reads the pipelines at the tip of a branch the way
// a deployment reads them.
func (c *Client) ReadConfiguredPipelines(ctx context.Context, org, workspace, branch string) (*ConfiguredPipelines, error) {
	var out ConfiguredPipelines
	if err := c.call(ctx, opReadConfiguredPipelines, []string{org, workspace}, url.Values{"branch": {branch}}, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDraft resumes the user's draft on a branch as it is, or starts an
// empty one at the branch tip.
func (c *Client) CreateDraft(ctx context.Context, s DraftScope) (*Draft, error) {
	var out Draft
	if err := c.call(ctx, opCreateConfigurationDraft, s.args(), nil, s.branch(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadDraft reads the user's draft on a branch.
func (c *Client) ReadDraft(ctx context.Context, s DraftScope) (*Draft, error) {
	var out Draft
	if err := c.call(ctx, opReadConfigurationDraft, s.args(), url.Values{"branch": {s.Branch}}, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SaveDraft replaces the user's draft with the draft version headSHA plus
// changes, rebased onto the branch tip.
func (c *Client) SaveDraft(ctx context.Context, s DraftScope, headSHA string, message *string, changes []FileChange) (*Draft, error) {
	body := struct {
		Branch  string       `json:"branch"`
		HeadSHA string       `json:"headSha"`
		Message *string      `json:"message,omitempty"`
		Changes []FileChange `json:"changes"`
	}{s.Branch, headSHA, message, changes}
	var out struct {
		Draft Draft `json:"draft"`
	}
	if err := c.call(ctx, opSaveConfigurationDraft, s.args(), nil, body, &out); err != nil {
		return nil, err
	}
	return &out.Draft, nil
}

// CommitDraft publishes the user's saved draft to its branch and returns the
// new tip.
func (c *Client) CommitDraft(ctx context.Context, s DraftScope) (string, error) {
	var out struct {
		Commit string `json:"commit"`
	}
	if err := c.call(ctx, opCommitConfigurationDraft, s.args(), nil, s.branch(), &out); err != nil {
		return "", err
	}
	return out.Commit, nil
}

// DiscardDraft removes the user's draft on a branch; it succeeds without one.
func (c *Client) DiscardDraft(ctx context.Context, s DraftScope) error {
	return c.call(ctx, opDiscardConfigurationDraft, s.args(), nil, s.branch(), nil)
}
