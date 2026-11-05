package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

var (
	opReadFileTree               = op("GET", "/organizations/{org}/workspaces/{workspace}/file-tree")
	opReadFile                   = op("GET", "/organizations/{org}/workspaces/{workspace}/file")
	opReadCommit                 = op("GET", "/organizations/{org}/workspaces/{workspace}/commits/detail")
	opReadConfiguredPipelines    = op("GET", "/organizations/{org}/workspaces/{workspace}/pipelines")
	opCommitConfigurationChanges = op("POST", "/organizations/{org}/workspaces/{workspace}/configuration/commits")
)

// TagHeadMoved is the error tag for a commit whose branch no longer points at
// the expected head.
const TagHeadMoved = "HeadMoved"

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

// FileChange puts a text file (Type "put") or deletes one (Type "delete").
type FileChange struct {
	Type string  `json:"type"`
	Path string  `json:"path"`
	Text *string `json:"text,omitempty"`
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

// ReadCommit reads a commit.
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

// CommitChanges commits changes to a branch, which must still point at
// expectedHead, and returns the new tip. Changes that leave every file as it
// is commit nothing and return the head.
func (c *Client) CommitChanges(ctx context.Context, org, workspace, branch, expectedHead, message string, changes []FileChange) (string, error) {
	body := struct {
		Branch       string       `json:"branch"`
		ExpectedHead string       `json:"expectedHead"`
		Message      string       `json:"message,omitempty"`
		Changes      []FileChange `json:"changes"`
	}{branch, expectedHead, message, changes}
	var out struct {
		Commit string `json:"commit"`
	}
	if err := c.call(ctx, opCommitConfigurationChanges, []string{org, workspace}, nil, body, &out); err != nil {
		return "", err
	}
	return out.Commit, nil
}

// MovedHead returns the current head that a HeadMoved error carries.
func MovedHead(err error) (string, bool) {
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Tag != TagHeadMoved {
		return "", false
	}
	var detail struct {
		Head string `json:"head"`
	}
	if json.Unmarshal(apiErr.body, &detail) != nil || detail.Head == "" {
		return "", false
	}
	return detail.Head, true
}
