package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &organizationMemberResource{}
	_ resource.ResourceWithImportState = &organizationMemberResource{}
	_ resource.ResourceWithModifyPlan  = &organizationMemberResource{}
)

func NewOrganizationMemberResource() resource.Resource {
	return &organizationMemberResource{}
}

type organizationMemberResource struct {
	client *client.Client
}

type organizationMemberModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	UserID         types.String `tfsdk:"user_id"`
	RoleIDs        types.Set    `tfsdk:"role_ids"`
}

func (r *organizationMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_member"
}

func (r *organizationMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Makes an existing user a member of an organization, without an invitation, and " +
			"keeps the member's roles to exactly `role_ids`. Removing the resource removes the membership.\n\n" +
			"Needs `Organization.addMember`, which only platform administrators hold, `Organization.listMembers`, " +
			"`OrganizationRole.assignToMember` and `OrganizationRole.revokeFromMember` on the roles, and " +
			"`OrganizationMember.remove`. Find a user's ID with the `tenzir_user` data source.\n\n" +
			"The platform refuses to add a user who already is a member; import such a member instead. Everyone who " +
			"signs up joins the demo organization, if the platform has one. Do not also manage the member's roles " +
			"with `tenzir_role_assignment`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The member ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the user.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role_ids": schema.SetAttribute{
				MarkdownDescription: "Built-in role IDs such as `org.member`, or custom roles' IDs. Defaults to none.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
		},
	}
}

func (r *organizationMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *organizationMemberResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, false)
}

// roleIDs returns the IDs of the member's roles as a set.
func roleIDs(ctx context.Context, member *client.Member) types.Set {
	ids := make([]string, len(member.Roles))
	for i, role := range member.Roles {
		ids[i] = role.ID
	}
	set, _ := types.SetValueFrom(ctx, types.StringType, ids)
	return set
}

func (r *organizationMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var roles []string
	resp.Diagnostics.Append(plan.RoleIDs.ElementsAs(ctx, &roles, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, organizationParent(plan.OrganizationID), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	member, err := r.client.AddMember(ctx, ids.Organization, client.NewMember{
		UserID:  plan.UserID.ValueString(),
		RoleIDs: roles,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot add the member", err.Error())
		return
	}
	plan.ID = types.StringValue(member.ID)
	plan.RoleIDs = roleIDs(ctx, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// find returns the member with the given ID, or nil if there is none.
func (r *organizationMemberResource) find(ctx context.Context, org string, match func(client.Member) bool) (*client.Member, error) {
	members, err := r.client.ListMembers(ctx, org)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if match(members[i]) {
			return &members[i], nil
		}
	}
	return nil, nil
}

func (r *organizationMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, err := r.find(ctx, ids.Organization, func(m client.Member) bool { return m.ID == state.ID.ValueString() })
	if client.IsNotFound(err) || (err == nil && member == nil) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization's members", err.Error())
		return
	}
	if member.User != nil {
		state.UserID = types.StringValue(member.User.ID)
	}
	state.RoleIDs = roleIDs(ctx, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var want, have []string
	resp.Diagnostics.Append(plan.RoleIDs.ElementsAs(ctx, &want, false)...)
	resp.Diagnostics.Append(state.RoleIDs.ElementsAs(ctx, &have, false)...)
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	memberID := state.ID.ValueString()
	// Assigning first keeps the member's recovery access while it swaps one
	// owner-like role for another.
	for _, role := range difference(want, have) {
		if err := r.client.AssignRoleTo(ctx, ids.Organization, memberID, role); err != nil {
			resp.Diagnostics.AddError("Cannot assign the role", fmt.Sprintf("%s: %s", role, err))
			return
		}
	}
	for _, role := range difference(have, want) {
		if err := r.client.RevokeRoleFrom(ctx, ids.Organization, memberID, role); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Cannot revoke the role", fmt.Sprintf("%s: %s", role, err))
			return
		}
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// difference returns the elements of a that b lacks.
func difference(a, b []string) []string {
	seen := make(map[string]bool, len(b))
	for _, s := range b {
		seen[s] = true
	}
	var out []string
	for _, s := range a {
		if !seen[s] {
			out = append(out, s)
		}
	}
	return out
}

func (r *organizationMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RemoveMember(ctx, ids.Organization, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot remove the member", err.Error())
	}
}

// ImportState takes "<organization>/<user>", with the organization by ID or
// slug, and stores IDs.
func (r *organizationMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "organization", "user")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	org, err := r.client.ReadOrganization(ctx, parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the member", err.Error())
		return
	}
	member, err := r.find(ctx, org.ID, func(m client.Member) bool { return m.User != nil && m.User.ID == parts[1] })
	if err == nil && member == nil {
		err = fmt.Errorf("user %s is no member of organization %s", parts[1], parts[0])
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the member", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), org.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), member.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), parts[1])...)
}
