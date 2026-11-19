package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// privateParents holds a resource's parents as IDs for API calls, because the
// state keeps the form that created it, which may be a slug that changes.
const privateParents = "parents"

var (
	organizationPath = path.Root("organization_id")
	workspacePath    = path.Root("workspace_id")
)

// parentIDs are the IDs of a resource's organization and, for resources in a
// workspace, of its workspace.
type parentIDs struct {
	Organization string `json:"organization"`
	Workspace    string `json:"workspace,omitempty"`
}

// parentRefs are a resource's parents as a configuration or state names
// them, each by ID or slug.
type parentRefs struct {
	Organization types.String
	Workspace    types.String
}

// organizationParent names the parent of a resource outside a workspace.
func organizationParent(org types.String) parentRefs {
	return parentRefs{Organization: org, Workspace: types.StringNull()}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// resolveParents skips the lookup for IDs, because the platform refuses slugs
// that look like IDs.
func resolveParents(ctx context.Context, c *client.Client, refs parentRefs) (parentIDs, error) {
	org, ws := refs.Organization.ValueString(), refs.Workspace.ValueString()
	if uuidPattern.MatchString(org) && (ws == "" || uuidPattern.MatchString(ws)) {
		return parentIDs{Organization: strings.ToLower(org), Workspace: strings.ToLower(ws)}, nil
	}
	if ws == "" {
		o, err := c.ReadOrganization(ctx, org)
		if err != nil {
			return parentIDs{}, err
		}
		return parentIDs{Organization: o.ID}, nil
	}
	w, err := c.ReadWorkspace(ctx, org, ws)
	if err != nil {
		return parentIDs{}, err
	}
	return parentIDs{Organization: w.OrgID, Workspace: w.ID}, nil
}

type privateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

type privateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// recordParents resolves the parents that Create plans, and records them.
func recordParents(ctx context.Context, c *client.Client, refs parentRefs, private privateSetter, diags *diag.Diagnostics) (parentIDs, bool) {
	ids, err := resolveParents(ctx, c, refs)
	if err != nil {
		diags.AddError("Cannot find the parents", err.Error())
		return ids, false
	}
	data, err := json.Marshal(ids)
	if err != nil {
		diags.AddError("Cannot record the parents", err.Error())
		return ids, false
	}
	diags.Append(private.SetKey(ctx, privateParents, data)...)
	return ids, !diags.HasError()
}

// knownParents falls back to the state's parents, which an import fills with
// IDs.
func knownParents(ctx context.Context, c *client.Client, private privateGetter, state parentRefs) (parentIDs, diag.Diagnostics) {
	data, diags := private.GetKey(ctx, privateParents)
	if diags.HasError() {
		return parentIDs{}, diags
	}
	var ids parentIDs
	if len(data) > 0 {
		if err := json.Unmarshal(data, &ids); err != nil {
			diags.AddError("Cannot read the recorded parents", err.Error())
		}
		return ids, diags
	}
	ids, err := resolveParents(ctx, c, state)
	if err != nil {
		diags.AddError("Cannot resolve the parents", err.Error())
	}
	return ids, diags
}

type attributeGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

func getParentRefs(ctx context.Context, data attributeGetter, inWorkspace bool) (parentRefs, diag.Diagnostics) {
	refs := parentRefs{Workspace: types.StringNull()}
	diags := data.GetAttribute(ctx, organizationPath, &refs.Organization)
	if inWorkspace {
		diags.Append(data.GetAttribute(ctx, workspacePath, &refs.Workspace)...)
	}
	return refs, diags
}

// planParents compares parents by ID, and keeps the state's form of a parent
// that the configuration names differently, so that nothing changes.
func planParents(ctx context.Context, c *client.Client, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, inWorkspace bool) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	config, diags := getParentRefs(ctx, req.Config, inWorkspace)
	resp.Diagnostics.Append(diags...)
	state, diags := getParentRefs(ctx, req.State, inWorkspace)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	paths := []path.Path{organizationPath}
	configured := []types.String{config.Organization}
	stored := []types.String{state.Organization}
	if inWorkspace {
		paths = append(paths, workspacePath)
		configured = append(configured, config.Workspace)
		stored = append(stored, state.Workspace)
	}
	if c == nil || config.Organization.IsUnknown() || config.Workspace.IsUnknown() {
		for i, p := range paths {
			if !configured[i].Equal(stored[i]) {
				resp.RequiresReplace.Append(p)
			}
		}
		return
	}
	have, diags := knownParents(ctx, c, req.Private, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	want, err := resolveParents(ctx, c, config)
	if err != nil {
		resp.Diagnostics.AddError("Cannot find the configured parents",
			fmt.Sprintf("%s. The resource belongs to %s. If a slug changed, name the parent by its ID or its new slug; "+
				"a parent that this apply renames or creates needs its ID, such as `tenzir_workspace.<name>.id`.", err, have))
		return
	}
	wantIDs := []string{want.Organization, want.Workspace}
	haveIDs := []string{have.Organization, have.Workspace}
	kept := false
	for i, p := range paths {
		switch {
		case wantIDs[i] == haveIDs[i]:
			if !configured[i].Equal(stored[i]) {
				resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, p, stored[i])...)
				kept = true
			}
		case configured[i].Equal(stored[i]):
			resp.Diagnostics.AddAttributeError(p, "Parent changed its slug",
				fmt.Sprintf("%q names %s now, but the resource belongs to %s. Name the parent by its ID or its new slug.",
					configured[i].ValueString(), wantIDs[i], haveIDs[i]))
		default:
			resp.RequiresReplace.Append(p)
		}
	}
	if kept && !resp.Diagnostics.HasError() {
		keepPriorState(req, resp)
	}
}

// keepPriorState undoes the unknown computed values that the framework
// planned before ModifyPlan, because a parent's form differed.
func keepPriorState(req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan, state, config map[string]tftypes.Value
	if resp.Plan.Raw.As(&plan) != nil || req.State.Raw.As(&state) != nil || req.Config.Raw.As(&config) != nil {
		return
	}
	for name, planned := range plan {
		if planned.Equal(state[name]) || (!planned.IsKnown() && config[name].IsNull()) {
			continue
		}
		return
	}
	resp.Plan.Raw = req.State.Raw.Copy()
}

func (ids parentIDs) String() string {
	if ids.Workspace == "" {
		return "organization " + ids.Organization
	}
	return "organization " + ids.Organization + " and workspace " + ids.Workspace
}
