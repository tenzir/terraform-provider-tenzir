package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &OrganizationMemberResource{}
var _ resource.ResourceWithImportState = &OrganizationMemberResource{}

func NewOrganizationMemberResource() resource.Resource {
	return &OrganizationMemberResource{}
}

// OrganizationMemberResource manages a user's membership in an organization.
type OrganizationMemberResource struct {
	client *client.Client
}

type OrganizationMemberResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	UserID         types.String `tfsdk:"user_id"`
	Role           types.String `tfsdk:"role"`
}

func (r *OrganizationMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_member"
}

func (r *OrganizationMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A user's membership in an organization. Requires org-admin " +
			"credentials. The platform enforces that a user belongs to at most one " +
			"organization, and rejects removing the last member or demoting or removing " +
			"the last admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identifier of the membership; identical to `user_id`, since " +
					"members are addressed by their user id.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Description: "Identifier of the organization.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Description: "Identifier of the user, as issued by the platform's identity " +
					"provider (e.g. the OIDC `sub` claim).",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Description: "Organization role of the member, `member` or `admin`. " +
					"Defaults to `member`. Changed in place.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("member"),
			},
		},
	}
}

func (r *OrganizationMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *OrganizationMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.AddOrganizationMember(ctx,
		plan.OrganizationID.ValueString(),
		plan.UserID.ValueString(),
		plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error adding organization member", err.Error())
		return
	}
	plan.ID = plan.UserID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	member, err := r.client.GetOrganizationMember(ctx,
		state.OrganizationID.ValueString(), state.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization member", err.Error())
		return
	}
	if member == nil {
		// The membership (or its whole organization) was removed outside of
		// Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = state.UserID
	state.Role = types.StringValue(member.Role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateOrganizationMemberRole(ctx,
		plan.OrganizationID.ValueString(),
		plan.UserID.ValueString(),
		plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating organization member role", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveOrganizationMember(ctx,
		state.OrganizationID.ValueString(), state.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error removing organization member", err.Error())
	}
}

func (r *OrganizationMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Memberships are addressed by organization and user id:
	// `terraform import tenzir_organization_member.example <organization-id>/<user-id>`
	organizationID, userID, ok := strings.Cut(req.ID, "/")
	if !ok || organizationID == "" || userID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <organization-id>/<user-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), organizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), userID)...)
}
