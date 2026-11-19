package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &roleAssignmentResource{}
	_ resource.ResourceWithImportState = &roleAssignmentResource{}
	_ resource.ResourceWithModifyPlan  = &roleAssignmentResource{}
)

func NewRoleAssignmentResource() resource.Resource {
	return &roleAssignmentResource{}
}

type roleAssignmentResource struct {
	client *client.Client
}

type roleAssignmentModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	MemberID       types.String `tfsdk:"member_id"`
	RoleID         types.String `tfsdk:"role_id"`
}

func (r *roleAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *roleAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns a built-in or custom role to a member of an organization. Removing the " +
			"resource revokes the role but keeps the membership and other roles.\n\n" +
			"Needs `OrganizationRole.assignToMember` and `OrganizationRole.revokeFromMember` on the role, and " +
			"`Organization.listMembers`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<organization_id>/<member_id>/<role_id>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"member_id": schema.StringAttribute{
				MarkdownDescription: "The ID of a member, which is not its user's ID; see the `tenzir_members` data source.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "A built-in role ID, `org.owner` or `org.member`, or a custom role's ID.",
				Required:            true,
				PlanModifiers:       replace,
			},
		},
	}
}

func (r *roleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *roleAssignmentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, false)
}

func (r *roleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, organizationParent(plan.OrganizationID), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	err := r.client.AssignRoleTo(ctx, ids.Organization, plan.MemberID.ValueString(), plan.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot assign the role", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.OrganizationID.ValueString() + "/" + plan.MemberID.ValueString() + "/" + plan.RoleID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	members, err := r.client.ListMembers(ctx, ids.Organization)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization's members", err.Error())
		return
	}
	for _, member := range members {
		if member.ID != state.MemberID.ValueString() {
			continue
		}
		for _, role := range member.Roles {
			if role.ID == state.RoleID.ValueString() {
				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
				return
			}
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *roleAssignmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected update", "Every attribute of a role assignment forces replacement. This is a bug in the provider.")
}

func (r *roleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RevokeRoleFrom(ctx, ids.Organization, state.MemberID.ValueString(), state.RoleID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot revoke the role", err.Error())
	}
}

// ImportState takes "<organization>/<member>/<role>".
func (r *roleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "organization", "member", "role")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &roleAssignmentModel{
		ID:             types.StringValue(req.ID),
		OrganizationID: types.StringValue(parts[0]),
		MemberID:       types.StringValue(parts[1]),
		RoleID:         types.StringValue(parts[2]),
	})...)
}
