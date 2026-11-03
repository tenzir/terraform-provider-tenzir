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
	UserID         types.String `tfsdk:"user_id"`
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
			"Needs `OrganizationRole.assignToUser` and `OrganizationRole.revokeFromUser` on the role, and " +
			"`Organization.listPeople`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<organization_id>/<user_id>/<role_id>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The ID of a member; see the `tenzir_people` data source.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "A built-in role ID such as `org.admin`, or a custom role's ID.",
				Required:            true,
				PlanModifiers:       replace,
			},
		},
	}
}

func (r *roleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *roleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.AssignMemberRole(ctx, plan.OrganizationID.ValueString(), plan.UserID.ValueString(), plan.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot assign the role", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.OrganizationID.ValueString() + "/" + plan.UserID.ValueString() + "/" + plan.RoleID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	people, err := r.client.ListPeople(ctx, state.OrganizationID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization's members", err.Error())
		return
	}
	for _, person := range people {
		if person.UserID != state.UserID.ValueString() {
			continue
		}
		for _, role := range person.Roles {
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
	err := r.client.RevokeMemberRole(ctx, state.OrganizationID.ValueString(), state.UserID.ValueString(), state.RoleID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot revoke the role", err.Error())
	}
}

// ImportState takes "<organization>/<user>/<role>".
func (r *roleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "organization", "user", "role")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &roleAssignmentModel{
		ID:             types.StringValue(req.ID),
		OrganizationID: types.StringValue(parts[0]),
		UserID:         types.StringValue(parts[1]),
		RoleID:         types.StringValue(parts[2]),
	})...)
}
