// Package client is a minimal HTTP client for the Tenzir Platform user API.
//
// Authentication model: the client is configured with an OIDC id_token and
// exchanges it for short-lived user keys. A key minted via /authenticate
// carries no workspace access and is used for account-level calls (managing
// organizations, creating workspaces). Workspace-scoped calls need a key
// minted via /switch-tenant, which is valid for exactly one workspace. The
// client transparently mints and caches one key per workspace and refreshes
// keys shortly before they expire.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// keyLifetimeSeconds is the lifetime requested for workspace-scoped keys.
// The server clamps this to at most 24 hours; one hour comfortably outlives
// a Terraform run while keeping the credential short-lived.
const keyLifetimeSeconds = 3600

// expiryMargin is how long before its expiry a cached key is considered
// stale and re-minted.
const expiryMargin = 60 * time.Second

// userKeyHeader carries the minted user key on authenticated requests.
const userKeyHeader = "X-Tenzir-UserKey"

// Client talks to the Tenzir Platform REST API.
//
// Exactly one credential mode is active. With an OIDC id_token, keys are
// minted via /authenticate and /switch-tenant; with an org-scoped service
// token via /service-token/authenticate; and with a federated workload
// identity token (id_token + federatedOrgID) via /federated/authenticate.
type Client struct {
	endpoint       string
	idToken        string
	serviceToken   string
	federatedOrgID string
	http           *http.Client

	mu            sync.Mutex
	accountKey    *mintedKey
	workspaceKeys map[string]*mintedKey
}

type mintedKey struct {
	value   string
	expires time.Time
}

func (k *mintedKey) stale() bool {
	if k == nil {
		return true
	}
	return !k.expires.IsZero() && time.Now().After(k.expires.Add(-expiryMargin))
}

// New creates a client authenticating with an OIDC id_token (user-identity
// credentials).
func New(endpoint, idToken string) *Client {
	return &Client{
		endpoint:      endpoint,
		idToken:       idToken,
		http:          &http.Client{Timeout: 30 * time.Second},
		workspaceKeys: make(map[string]*mintedKey),
	}
}

// NewWithServiceToken creates a client authenticating with an org-scoped
// service token.
func NewWithServiceToken(endpoint, serviceToken string) *Client {
	return &Client{
		endpoint:      endpoint,
		serviceToken:  serviceToken,
		http:          &http.Client{Timeout: 30 * time.Second},
		workspaceKeys: make(map[string]*mintedKey),
	}
}

// NewFederated creates a client authenticating with a federated workload
// identity token (e.g. a CI-issued OIDC token) against the given
// organization's trusted-issuer configuration.
func NewFederated(endpoint, idToken, organizationID string) *Client {
	return &Client{
		endpoint:       endpoint,
		idToken:        idToken,
		federatedOrgID: organizationID,
		http:           &http.Client{Timeout: 30 * time.Second},
		workspaceKeys:  make(map[string]*mintedKey),
	}
}

// APIError is returned for non-2xx responses.
type APIError struct {
	Status  int
	Path    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("POST %s: status %d: %s", e.Path, e.Status, e.Message)
}

// IsNotFound reports whether err is an API response with status 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// post sends a JSON request and decodes the JSON response into out (unless
// out is nil). userKey is added as authentication if non-empty.
func (c *Client) post(ctx context.Context, path, userKey string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if userKey != "" {
		req.Header.Set(userKeyHeader, userKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Path: path, Message: string(msg)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type keyResponse struct {
	UserKey string `json:"user_key"`
	Expires *int64 `json:"expires"`
}

func (r keyResponse) minted() *mintedKey {
	k := &mintedKey{value: r.UserKey}
	if r.Expires != nil {
		k.expires = time.Unix(*r.Expires, 0)
	}
	return k
}

// accountUserKey returns a cached or freshly minted key without workspace
// access, for account-level endpoints.
func (c *Client) accountUserKey(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accountKey.stale() {
		var resp keyResponse
		var err error
		switch {
		case c.serviceToken != "":
			err = c.post(ctx, "/service-token/authenticate", "",
				map[string]any{"service_token": c.serviceToken}, &resp)
		case c.federatedOrgID != "":
			err = c.post(ctx, "/federated/authenticate", "", map[string]any{
				"id_token":        c.idToken,
				"organization_id": c.federatedOrgID,
			}, &resp)
		default:
			err = c.post(ctx, "/authenticate", "",
				map[string]any{"id_token": c.idToken}, &resp)
		}
		if err != nil {
			return "", fmt.Errorf("authenticating: %w", err)
		}
		c.accountKey = resp.minted()
	}
	return c.accountKey.value, nil
}

// workspaceUserKey returns a cached or freshly minted key scoped to the
// given workspace. Returns a 404 APIError if the workspace does not exist.
func (c *Client) workspaceUserKey(ctx context.Context, workspaceID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.workspaceKeys[workspaceID].stale() {
		var resp keyResponse
		var err error
		switch {
		case c.serviceToken != "":
			err = c.post(ctx, "/service-token/authenticate", "", map[string]any{
				"service_token":              c.serviceToken,
				"tenant_id":                  workspaceID,
				"requested_lifetime_seconds": keyLifetimeSeconds,
			}, &resp)
		case c.federatedOrgID != "":
			err = c.post(ctx, "/federated/authenticate", "", map[string]any{
				"id_token":                   c.idToken,
				"organization_id":            c.federatedOrgID,
				"tenant_id":                  workspaceID,
				"requested_lifetime_seconds": keyLifetimeSeconds,
			}, &resp)
		default:
			err = c.post(ctx, "/switch-tenant", "", map[string]any{
				"id_token":                   c.idToken,
				"tenant_id":                  workspaceID,
				"requested_lifetime_seconds": keyLifetimeSeconds,
			}, &resp)
		}
		if err != nil {
			return "", fmt.Errorf("switching to workspace %s: %w", workspaceID, err)
		}
		c.workspaceKeys[workspaceID] = resp.minted()
	}
	return c.workspaceKeys[workspaceID].value, nil
}

// accountPost calls an account-level endpoint.
func (c *Client) accountPost(ctx context.Context, path string, in, out any) error {
	key, err := c.accountUserKey(ctx)
	if err != nil {
		return err
	}
	return c.post(ctx, path, key, in, out)
}

// workspacePost calls a workspace-scoped endpoint.
func (c *Client) workspacePost(ctx context.Context, workspaceID, path string, in, out any) error {
	key, err := c.workspaceUserKey(ctx, workspaceID)
	if err != nil {
		return err
	}
	return c.post(ctx, path, key, in, out)
}

// --- Organizations ---

type Organization struct {
	ID         string `json:"organization_id"`
	Name       string `json:"name"`
	IconURL    string `json:"icon_url"`
	RequireMFA bool   `json:"require_mfa"`
}

func (c *Client) CreateOrganization(ctx context.Context, name, iconURL string) (string, error) {
	var out struct {
		OrganizationID string `json:"organization_id"`
	}
	in := map[string]any{"name": name, "icon_url": iconURL}
	if err := c.accountPost(ctx, "/org/create", in, &out); err != nil {
		return "", err
	}
	return out.OrganizationID, nil
}

// GetOrganization returns nil without error if the organization does not exist.
func (c *Client) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	var out Organization
	in := map[string]any{"organization_id": id}
	err := c.accountPost(ctx, "/org/get", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// OrganizationUpdate carries the fields to change; nil fields are left as-is.
type OrganizationUpdate struct {
	Name       *string `json:"name,omitempty"`
	IconURL    *string `json:"icon_url,omitempty"`
	RequireMFA *bool   `json:"require_mfa,omitempty"`
}

func (c *Client) UpdateOrganization(ctx context.Context, id string, update OrganizationUpdate) error {
	in := struct {
		OrganizationID string `json:"organization_id"`
		OrganizationUpdate
	}{OrganizationID: id, OrganizationUpdate: update}
	return c.accountPost(ctx, "/org/update", in, nil)
}

func (c *Client) DeleteOrganization(ctx context.Context, id string) error {
	return c.accountPost(ctx, "/org/delete", map[string]any{"organization_id": id}, nil)
}

// --- Federated credentials ---

// FederatedCredential is an organization-level OIDC workload identity
// federation credential. External tokens matching Issuer, Audience, and all
// ClaimConditions can be exchanged for platform keys acting as an org member
// with the configured role.
type FederatedCredential struct {
	ID              string            `json:"credential_id"`
	Label           string            `json:"label"`
	Issuer          string            `json:"issuer"`
	Audience        string            `json:"audience"`
	ClaimConditions map[string]string `json:"claim_conditions"`
	Role            string            `json:"role"`
}

func (c *Client) AddFederatedCredential(ctx context.Context, organizationID string, cred FederatedCredential) (string, error) {
	var out struct {
		CredentialID string `json:"credential_id"`
	}
	in := map[string]any{
		"organization_id":  organizationID,
		"label":            cred.Label,
		"issuer":           cred.Issuer,
		"audience":         cred.Audience,
		"claim_conditions": cred.ClaimConditions,
		"role":             cred.Role,
	}
	if err := c.accountPost(ctx, "/org/add-federated-credential", in, &out); err != nil {
		return "", err
	}
	return out.CredentialID, nil
}

// GetFederatedCredential returns nil without error if the credential (or the
// organization) does not exist. There is no single-credential endpoint, so
// this filters the organization's credential list.
func (c *Client) GetFederatedCredential(ctx context.Context, organizationID, credentialID string) (*FederatedCredential, error) {
	var out struct {
		Credentials []FederatedCredential `json:"credentials"`
	}
	in := map[string]any{"organization_id": organizationID}
	err := c.accountPost(ctx, "/org/list-federated-credentials", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, cred := range out.Credentials {
		if cred.ID == credentialID {
			return &cred, nil
		}
	}
	return nil, nil
}

func (c *Client) RemoveFederatedCredential(ctx context.Context, organizationID, credentialID string) error {
	in := map[string]any{"organization_id": organizationID, "credential_id": credentialID}
	return c.accountPost(ctx, "/org/remove-federated-credential", in, nil)
}

// --- Library sources ---

// LibrarySource is an organization-level library source: a GitHub repository
// from which packages are offered in the organization's workspace libraries.
type LibrarySource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	GitHubURL   string `json:"github_url"`
	BranchOrRef string `json:"branch_or_ref"`
	// Name of an org-scoped platform secret holding the access token for a
	// private repository; nil means a public repository.
	CredentialSecretName *string `json:"credential_secret_name"`
}

func (c *Client) AddLibrarySource(ctx context.Context, organizationID string, source LibrarySource) (string, error) {
	var out struct {
		SourceID string `json:"source_id"`
	}
	in := map[string]any{
		"organization_id":        organizationID,
		"name":                   source.Name,
		"github_url":             source.GitHubURL,
		"branch_or_ref":          source.BranchOrRef,
		"credential_secret_name": source.CredentialSecretName,
	}
	if err := c.accountPost(ctx, "/org/add-library-source", in, &out); err != nil {
		return "", err
	}
	return out.SourceID, nil
}

// GetLibrarySource returns nil without error if the source (or the
// organization) does not exist. There is no single-source endpoint, so this
// filters the organization's source list. The list also contains
// platform-configured default sources (with synthesized `lib-default-...`
// ids) that are not part of the organization; matching on the exact id of a
// source created via AddLibrarySource never picks those up.
func (c *Client) GetLibrarySource(ctx context.Context, organizationID, sourceID string) (*LibrarySource, error) {
	var out struct {
		Sources []LibrarySource `json:"sources"`
	}
	in := map[string]any{"organization_id": organizationID}
	err := c.accountPost(ctx, "/org/list-library-sources", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, s := range out.Sources {
		if s.ID == sourceID {
			return &s, nil
		}
	}
	return nil, nil
}

// UpdateLibrarySource replaces the settings of the source identified by
// source.ID with the remaining fields.
func (c *Client) UpdateLibrarySource(ctx context.Context, organizationID string, source LibrarySource) error {
	in := map[string]any{
		"organization_id":        organizationID,
		"source_id":              source.ID,
		"name":                   source.Name,
		"github_url":             source.GitHubURL,
		"branch_or_ref":          source.BranchOrRef,
		"credential_secret_name": source.CredentialSecretName,
	}
	return c.accountPost(ctx, "/org/update-library-source", in, nil)
}

func (c *Client) RemoveLibrarySource(ctx context.Context, organizationID, sourceID string) error {
	in := map[string]any{"organization_id": organizationID, "source_id": sourceID}
	return c.accountPost(ctx, "/org/remove-library-source", in, nil)
}

// --- Organization members ---

// OrganizationMember is a user's membership in an organization. Members are
// identified by their user id; the platform enforces that a user belongs to
// at most one organization.
type OrganizationMember struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	// Email and Name come from the user's profile, which the platform only
	// stores after the user has logged in at least once; nil otherwise.
	Email *string `json:"email"`
	Name  *string `json:"name"`
}

// AddOrganizationMember adds a user to an organization with the given role.
// Fails if the user already belongs to an organization.
func (c *Client) AddOrganizationMember(ctx context.Context, organizationID, userID, role string) error {
	in := map[string]any{"organization_id": organizationID, "user_id": userID, "role": role}
	return c.accountPost(ctx, "/org/add-member", in, nil)
}

// ListOrganizationMembers returns all members of an organization.
func (c *Client) ListOrganizationMembers(ctx context.Context, organizationID string) ([]OrganizationMember, error) {
	var out struct {
		Members []OrganizationMember `json:"members"`
	}
	in := map[string]any{"organization_id": organizationID}
	if err := c.accountPost(ctx, "/org/list-members", in, &out); err != nil {
		return nil, err
	}
	return out.Members, nil
}

// GetOrganizationMember returns nil without error if the membership (or the
// organization) does not exist. There is no single-member endpoint, so this
// filters the organization's member list.
func (c *Client) GetOrganizationMember(ctx context.Context, organizationID, userID string) (*OrganizationMember, error) {
	members, err := c.ListOrganizationMembers(ctx, organizationID)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.UserID == userID {
			return &m, nil
		}
	}
	return nil, nil
}

// UpdateOrganizationMemberRole changes a member's role in place.
func (c *Client) UpdateOrganizationMemberRole(ctx context.Context, organizationID, userID, role string) error {
	in := map[string]any{"organization_id": organizationID, "user_id": userID, "role": role}
	return c.accountPost(ctx, "/org/update-member-role", in, nil)
}

// RemoveOrganizationMember removes a user from an organization. The platform
// rejects removing the last member or the last admin.
func (c *Client) RemoveOrganizationMember(ctx context.Context, organizationID, userID string) error {
	in := map[string]any{"organization_id": organizationID, "user_id": userID}
	return c.accountPost(ctx, "/org/remove-member", in, nil)
}

// --- Service tokens ---

// ServiceToken is an organization-level static machine credential acting
// inside its organization as a synthetic member with the configured role.
// The plaintext secret is returned exactly once at creation time; the
// platform only persists its hash, so it can never be retrieved again.
type ServiceToken struct {
	ID        string  `json:"token_id"`
	Label     string  `json:"label"`
	Role      string  `json:"role"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"` // nil means the token never expires
}

// CreateServiceToken creates a service token and returns its id and the
// plaintext secret. A nil expiresInSeconds creates a token that never
// expires.
func (c *Client) CreateServiceToken(ctx context.Context, organizationID, label, role string, expiresInSeconds *int64) (id, secret string, err error) {
	var out struct {
		TokenID string `json:"token_id"`
		Secret  string `json:"secret"`
	}
	in := map[string]any{
		"organization_id": organizationID,
		"label":           label,
		"role":            role,
	}
	if expiresInSeconds != nil {
		in["expires_in_seconds"] = *expiresInSeconds
	}
	if err := c.accountPost(ctx, "/org/create-service-token", in, &out); err != nil {
		return "", "", err
	}
	return out.TokenID, out.Secret, nil
}

// GetServiceToken returns nil without error if the token (or the
// organization) does not exist. There is no single-token endpoint, so this
// filters the organization's token list. The result never contains the
// plaintext secret.
func (c *Client) GetServiceToken(ctx context.Context, organizationID, tokenID string) (*ServiceToken, error) {
	var out struct {
		Tokens []ServiceToken `json:"tokens"`
	}
	in := map[string]any{"organization_id": organizationID}
	err := c.accountPost(ctx, "/org/list-service-tokens", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, t := range out.Tokens {
		if t.ID == tokenID {
			return &t, nil
		}
	}
	return nil, nil
}

// RevokeServiceToken revokes (deletes) a service token. Already-minted
// workspace-scoped keys stay valid until they expire.
func (c *Client) RevokeServiceToken(ctx context.Context, organizationID, tokenID string) error {
	in := map[string]any{"organization_id": organizationID, "token_id": tokenID}
	return c.accountPost(ctx, "/org/revoke-service-token", in, nil)
}

// --- Workspaces ---

type WorkspaceOwner struct {
	Namespace   string `json:"namespace"`
	OwnerID     string `json:"owner_id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	ID      string         `json:"tenant_id"`
	Name    string         `json:"name"`
	IconURL string         `json:"icon_url"`
	Owner   WorkspaceOwner `json:"owner"`
}

// CreateWorkspace creates a workspace owned by the authenticated user's
// organization.
func (c *Client) CreateWorkspace(ctx context.Context, name, iconURL string) (string, error) {
	var out struct {
		TenantID string `json:"tenant_id"`
	}
	in := map[string]any{"name": name, "icon_url": iconURL, "org_owned": true}
	if err := c.accountPost(ctx, "/workspace/create", in, &out); err != nil {
		return "", err
	}
	return out.TenantID, nil
}

// GetWorkspace returns nil without error if the workspace does not exist.
func (c *Client) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	var out Workspace
	err := c.workspacePost(ctx, id, "/get-tenant", map[string]any{"tenant_id": id}, &out)
	// A deleted workspace surfaces as 404 either from /switch-tenant (while
	// minting the workspace key) or from /get-tenant itself.
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RenameWorkspace(ctx context.Context, id, name string) error {
	in := map[string]any{"tenant_id": id, "name": name}
	return c.workspacePost(ctx, id, "/rename-tenant", in, nil)
}

func (c *Client) UpdateWorkspaceIcon(ctx context.Context, id, iconURL string) error {
	in := map[string]any{"tenant_id": id, "icon_url": iconURL}
	return c.workspacePost(ctx, id, "/update-tenant-icon", in, nil)
}

func (c *Client) DeleteWorkspace(ctx context.Context, id string) error {
	in := map[string]any{"tenant_id": id, "reason": "deleted via terraform"}
	return c.workspacePost(ctx, id, "/delete-tenant", in, nil)
}

// --- Workspace auth rules ---

// AddWorkspaceAuthRule adds an access or admin rule to a workspace and
// returns the rule's id. target is "access" or "admin"; rule is the
// serialized auth function object (a JSON object with an `auth_fn`
// discriminator field, e.g. {"auth_fn": "auth_user", "user_id": "..."}).
// Requires workspace admin permissions.
func (c *Client) AddWorkspaceAuthRule(ctx context.Context, workspaceID, target string, rule json.RawMessage) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	in := map[string]any{
		"tenant_id": workspaceID,
		"target":    target,
		"auth_fn":   rule,
	}
	if err := c.workspacePost(ctx, workspaceID, "/workspace/add-auth-rule", in, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// GetWorkspaceAuthRule returns the serialized auth function of the rule with
// the given id in the given target list ("access" or "admin"), or nil
// without error if the rule (or the whole workspace) does not exist. There
// is no single-rule endpoint, so this filters the workspace's rule list. The
// platform embeds the rule id in the serialized auth function on list; it is
// stripped here so the result matches the representation passed to
// AddWorkspaceAuthRule.
func (c *Client) GetWorkspaceAuthRule(ctx context.Context, workspaceID, target, ruleID string) (json.RawMessage, error) {
	type ruleDescription struct {
		ID     string         `json:"id"`
		AuthFn map[string]any `json:"auth_fn"`
	}
	var out struct {
		AuthFunctions  []ruleDescription `json:"auth_functions"`
		AdminFunctions []ruleDescription `json:"admin_functions"`
	}
	in := map[string]any{"tenant_id": workspaceID}
	err := c.workspacePost(ctx, workspaceID, "/workspace/list-auth-rules", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rules := out.AuthFunctions
	if target == "admin" {
		rules = out.AdminFunctions
	}
	for _, r := range rules {
		if r.ID == ruleID {
			delete(r.AuthFn, "id")
			return json.Marshal(r.AuthFn)
		}
	}
	return nil, nil
}

// RemoveWorkspaceAuthRule removes an access or admin rule from a workspace.
// The platform rejects removing the last admin rule of a workspace. Requires
// workspace admin permissions.
func (c *Client) RemoveWorkspaceAuthRule(ctx context.Context, workspaceID, target, ruleID string) error {
	in := map[string]any{"tenant_id": workspaceID, "target": target, "id": ruleID}
	return c.workspacePost(ctx, workspaceID, "/workspace/remove-auth-rule", in, nil)
}

// --- Nodes ---

type Node struct {
	ID   string `json:"node_id"`
	Name string `json:"name"`
	// Whether the node was created as a demo node.
	Demo bool `json:"demo"`
	// Whether the node was created as an ephemeral node.
	Ephemeral bool `json:"ephemeral"`
}

func (c *Client) CreateNode(ctx context.Context, workspaceID, name string) (string, error) {
	var out struct {
		NodeID string `json:"node_id"`
	}
	in := map[string]any{"tenant_id": workspaceID, "node_name": name}
	if err := c.workspacePost(ctx, workspaceID, "/create-node", in, &out); err != nil {
		return "", err
	}
	return out.NodeID, nil
}

// ListNodes returns the stored metadata of all nodes in a workspace,
// without performing a live connectivity check.
func (c *Client) ListNodes(ctx context.Context, workspaceID string) ([]Node, error) {
	var out struct {
		Nodes []Node `json:"nodes"`
	}
	in := map[string]any{"tenant_id": workspaceID}
	if err := c.workspacePost(ctx, workspaceID, "/list-nodes-v3", in, &out); err != nil {
		return nil, err
	}
	return out.Nodes, nil
}

// GetNode returns nil without error if the node (or its workspace) does not
// exist. There is no single-node endpoint, so this filters the node list of
// the workspace.
func (c *Client) GetNode(ctx context.Context, workspaceID, nodeID string) (*Node, error) {
	nodes, err := c.ListNodes(ctx, workspaceID)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.ID == nodeID {
			return &n, nil
		}
	}
	return nil, nil
}

// GetNodeToken returns the authentication token a Tenzir node uses to
// connect to the platform. The token is stable: repeated calls return the
// same token.
func (c *Client) GetNodeToken(ctx context.Context, workspaceID, nodeID string) (string, error) {
	var out struct {
		TenzirToken string `json:"tenzir_token"`
	}
	in := map[string]any{"tenant_id": workspaceID, "node_id": nodeID}
	if err := c.workspacePost(ctx, workspaceID, "/get-node-token", in, &out); err != nil {
		return "", err
	}
	return out.TenzirToken, nil
}

func (c *Client) RenameNode(ctx context.Context, workspaceID, nodeID, name string) error {
	in := map[string]any{"tenant_id": workspaceID, "node_id": nodeID, "node_name": name}
	return c.workspacePost(ctx, workspaceID, "/rename-node", in, nil)
}

func (c *Client) DeleteNode(ctx context.Context, workspaceID, nodeID string) error {
	in := map[string]any{"tenant_id": workspaceID, "node_id": nodeID}
	return c.workspacePost(ctx, workspaceID, "/delete-node", in, nil)
}

// --- Alerts ---

// Alert is a node-offline alert: when the node has been disconnected for
// Duration seconds, the platform POSTs WebhookBody to WebhookURL. The
// platform supports at most one alert per node.
type Alert struct {
	ID          string `json:"id"`
	NodeID      string `json:"node_id"`
	Duration    int64  `json:"duration"` // seconds
	WebhookURL  string `json:"webhook_url"`
	WebhookBody string `json:"webhook_body"` // must be valid JSON
}

// AddAlert creates a node-offline alert and returns its id.
func (c *Client) AddAlert(ctx context.Context, workspaceID string, alert Alert) (string, error) {
	var out struct {
		AlertID string `json:"alert_id"`
	}
	in := map[string]any{
		"tenant_id":    workspaceID,
		"node_id":      alert.NodeID,
		"duration":     alert.Duration,
		"webhook_url":  alert.WebhookURL,
		"webhook_body": alert.WebhookBody,
	}
	if err := c.workspacePost(ctx, workspaceID, "/alert/add", in, &out); err != nil {
		return "", err
	}
	return out.AlertID, nil
}

// GetAlert returns nil without error if the alert (or the whole workspace)
// does not exist. There is no single-alert endpoint, so this filters the
// workspace's alert list.
func (c *Client) GetAlert(ctx context.Context, workspaceID, alertID string) (*Alert, error) {
	var out struct {
		Alerts []Alert `json:"alerts"`
	}
	in := map[string]any{"tenant_id": workspaceID}
	err := c.workspacePost(ctx, workspaceID, "/alert/list", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, a := range out.Alerts {
		if a.ID == alertID {
			return &a, nil
		}
	}
	return nil, nil
}

// DeleteAlert deletes an alert.
func (c *Client) DeleteAlert(ctx context.Context, workspaceID, alertID string) error {
	in := map[string]any{"tenant_id": workspaceID, "alert_id": alertID}
	return c.workspacePost(ctx, workspaceID, "/alert/delete", in, nil)
}

// --- Secrets ---

// Secret is the server-side metadata of a workspace secret. The platform
// never returns secret values.
type Secret struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AddSecret stores a new secret in the given secret store, or in the
// workspace's current default store if storeID is nil. Returns the secret id.
func (c *Client) AddSecret(ctx context.Context, workspaceID string, storeID *string, name, value string) (string, error) {
	var out struct {
		SecretID string `json:"secret_id"`
	}
	in := map[string]any{"tenant_id": workspaceID, "name": name, "value": value}
	if storeID != nil {
		in["store_id"] = *storeID
	}
	if err := c.workspacePost(ctx, workspaceID, "/secrets/add", in, &out); err != nil {
		return "", err
	}
	return out.SecretID, nil
}

// GetSecret returns nil without error if the secret (or its store, or the
// whole workspace) does not exist. There is no single-secret endpoint, so
// this filters the store's secret list. A nil storeID lists the workspace's
// current default store.
func (c *Client) GetSecret(ctx context.Context, workspaceID string, storeID *string, secretID string) (*Secret, error) {
	var out struct {
		Secrets []Secret `json:"secrets"`
	}
	in := map[string]any{"tenant_id": workspaceID}
	if storeID != nil {
		in["store_id"] = *storeID
	}
	err := c.workspacePost(ctx, workspaceID, "/secrets/list", in, &out)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, s := range out.Secrets {
		if s.ID == secretID {
			return &s, nil
		}
	}
	return nil, nil
}

// UpdateSecret replaces the value of an existing secret. Secrets cannot be
// renamed.
func (c *Client) UpdateSecret(ctx context.Context, workspaceID string, storeID *string, secretID, value string) error {
	in := map[string]any{"tenant_id": workspaceID, "secret_id": secretID, "value": value}
	if storeID != nil {
		in["store_id"] = *storeID
	}
	return c.workspacePost(ctx, workspaceID, "/secrets/update", in, nil)
}

// RemoveSecret deletes a secret.
func (c *Client) RemoveSecret(ctx context.Context, workspaceID string, storeID *string, secretID string) error {
	in := map[string]any{"tenant_id": workspaceID, "secret_id": secretID}
	if storeID != nil {
		in["store_id"] = *storeID
	}
	return c.workspacePost(ctx, workspaceID, "/secrets/remove", in, nil)
}

// --- Secret stores ---

// SecretStore is a workspace secret store. Type is "aws", "vault", or
// "tenzir"; the latter is the platform-managed built-in store that exists in
// every workspace and cannot be created or deleted through the API.
type SecretStore struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	IsWritable  bool               `json:"is_writable"`
	IsDeletable bool               `json:"is_deletable"`
	Options     SecretStoreOptions `json:"options"`
}

// SecretStoreOptions merges the per-type option fields of a secret store;
// fields not applicable to the store's type stay zero. Credential values
// (the Vault token and approle secret id) are masked by the platform on
// read.
type SecretStoreOptions struct {
	// AWS Secrets Manager options.
	Region         string `json:"region"`
	AssumedRoleARN string `json:"assumed_role_arn"`
	// HashiCorp Vault options.
	Address     string            `json:"address"`
	Namespace   *string           `json:"namespace"`
	Mount       string            `json:"mount"`
	AuthMethod  string            `json:"auth_method"`
	TokenAuth   *VaultTokenAuth   `json:"token_auth"`
	AppRoleAuth *VaultAppRoleAuth `json:"approle_auth"`
}

type VaultTokenAuth struct {
	Token string `json:"token"` // masked on read
}

type VaultAppRoleAuth struct {
	RoleID   string `json:"role_id"`
	SecretID string `json:"secret_id"` // masked on read
}

// AddSecretStore registers an external secret store in a workspace and
// returns the store id. storeType is "aws" or "vault"; options is the flat
// per-type option map defined by the API. A nil name selects a server-side
// default name. With makeDefault, the new store becomes the workspace's
// default secret store.
func (c *Client) AddSecretStore(ctx context.Context, workspaceID, storeType string, name *string, isWritable, makeDefault bool, options map[string]string) (string, error) {
	var out struct {
		StoreID string `json:"store_id"`
	}
	in := map[string]any{
		"tenant_id":    workspaceID,
		"type":         storeType,
		"is_writable":  isWritable,
		"make_default": makeDefault,
		"options":      options,
	}
	if name != nil {
		in["name"] = *name
	}
	if err := c.workspacePost(ctx, workspaceID, "/secrets/add-external-store", in, &out); err != nil {
		return "", err
	}
	return out.StoreID, nil
}

// GetSecretStore returns the store and whether it is the workspace's default
// secret store; nil without error if the store (or the whole workspace) does
// not exist. There is no single-store endpoint, so this filters the
// workspace's store list.
func (c *Client) GetSecretStore(ctx context.Context, workspaceID, storeID string) (*SecretStore, bool, error) {
	var out struct {
		DefaultStoreID string        `json:"default_store_id"`
		Stores         []SecretStore `json:"stores"`
	}
	in := map[string]any{"tenant_id": workspaceID}
	err := c.workspacePost(ctx, workspaceID, "/secrets/list-stores", in, &out)
	if IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	for _, s := range out.Stores {
		if s.ID == storeID {
			return &s, s.ID == out.DefaultStoreID, nil
		}
	}
	return nil, false, nil
}

// SelectSecretStore makes the given store the workspace's default secret
// store. There is no inverse operation: the default can only be moved to
// another existing store, never unset.
func (c *Client) SelectSecretStore(ctx context.Context, workspaceID, storeID string) error {
	in := map[string]any{"tenant_id": workspaceID, "store_id": storeID}
	return c.workspacePost(ctx, workspaceID, "/secrets/select-store", in, nil)
}

// DeleteSecretStore removes an external secret store. The platform rejects
// deleting the workspace's current default store and the built-in store.
func (c *Client) DeleteSecretStore(ctx context.Context, workspaceID, storeID string) error {
	in := map[string]any{"tenant_id": workspaceID, "store_id": storeID}
	return c.workspacePost(ctx, workspaceID, "/secrets/delete-external-store", in, nil)
}
