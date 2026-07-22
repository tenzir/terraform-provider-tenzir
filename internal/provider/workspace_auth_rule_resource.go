package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &WorkspaceAuthRuleResource{}
var _ resource.ResourceWithImportState = &WorkspaceAuthRuleResource{}

func NewWorkspaceAuthRuleResource() resource.Resource {
	return &WorkspaceAuthRuleResource{}
}

// WorkspaceAuthRuleResource manages an access or admin rule of a Tenzir
// Platform workspace.
type WorkspaceAuthRuleResource struct {
	client *client.Client
}

type WorkspaceAuthRuleResourceModel struct {
	ID          types.String         `tfsdk:"id"`
	WorkspaceID types.String         `tfsdk:"workspace_id"`
	Target      types.String         `tfsdk:"target"`
	Rule        jsontypes.Normalized `tfsdk:"rule"`
}

func (r *WorkspaceAuthRuleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_auth_rule"
}

func (r *WorkspaceAuthRuleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An access or admin rule of a Tenzir Platform workspace. Access rules " +
			"(`target = \"access\"`) control who can use the workspace; admin rules " +
			"(`target = \"admin\"`) control who can manage it. Requires workspace admin " +
			"permissions.\n\n" +
			"The platform has no update operation for auth rules, so changing any " +
			"attribute replaces the rule. The platform rejects removing the last admin " +
			"rule of a workspace, and personal workspaces only accept `auth_user` rules.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the rule.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Description: "Identifier of the workspace the rule belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target": schema.StringAttribute{
				Description: "Which rule list the rule is added to: `access` (who can use " +
					"the workspace) or `admin` (who can manage it).",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"rule": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "The rule definition as a JSON object with an `auth_fn` " +
					"discriminator field selecting the rule type, plus type-specific " +
					"fields. Examples: `{\"auth_fn\": \"auth_user\", \"user_id\": \"...\"}` " +
					"grants a single user, `{\"auth_fn\": \"auth_email_suffix\", " +
					"\"email_domain\": \"@example.com\"}` grants users by email domain, " +
					"`{\"auth_fn\": \"auth_org_member\", \"org_id\": \"...\"}` grants all " +
					"members of a platform organization. Do not include an `id` field; the " +
					"platform assigns rule ids. Prefer `jsonencode()` over a raw string.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *WorkspaceAuthRuleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *WorkspaceAuthRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceAuthRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.AddWorkspaceAuthRule(ctx, plan.WorkspaceID.ValueString(),
		plan.Target.ValueString(), []byte(plan.Rule.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace auth rule", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceAuthRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceAuthRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rule, err := r.client.GetWorkspaceAuthRule(ctx, state.WorkspaceID.ValueString(),
		state.Target.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace auth rule", err.Error())
		return
	}
	if rule == nil {
		// The rule (or its whole workspace) was deleted outside of Terraform;
		// plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	// Rules are immutable server-side, so an existing rule's definition
	// cannot have drifted; keep the configured representation. Only after an
	// import (when no representation is known yet) the state is populated
	// with the platform's serialization, which spells out optional fields
	// left at their defaults (e.g. `"connection": null`).
	if state.Rule.IsNull() {
		state.Rule = jsontypes.NewNormalizedValue(string(rule))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceAuthRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Unreachable: every attribute carries a RequiresReplace plan modifier,
	// since the platform has no update endpoint for workspace auth rules.
	resp.Diagnostics.AddError(
		"Workspace auth rules cannot be updated in place",
		"This is a bug in the provider: all attribute changes should have planned a replacement.",
	)
}

func (r *WorkspaceAuthRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceAuthRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveWorkspaceAuthRule(ctx, state.WorkspaceID.ValueString(),
		state.Target.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting workspace auth rule", err.Error())
	}
}

func (r *WorkspaceAuthRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Auth rules are addressed by workspace, target list, and rule id:
	// `terraform import tenzir_workspace_auth_rule.example <workspace-id>/<target>/<rule-id>`
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" ||
		(parts[1] != "access" && parts[1] != "admin") {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <workspace-id>/<target>/<rule-id> "+
				"with target `access` or `admin`, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}
