package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &OrganizationResource{}
var _ resource.ResourceWithImportState = &OrganizationResource{}

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

// OrganizationResource manages a Tenzir Platform organization.
type OrganizationResource struct {
	client *client.Client
}

type OrganizationResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	IconURL    types.String `tfsdk:"icon_url"`
	RequireMFA types.Bool   `tfsdk:"require_mfa"`
}

func (r *OrganizationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An organization in the Tenzir Platform. The user authenticated by the " +
			"provider becomes the initial admin of the organization.\n\n" +
			"~> Creating an organization requires user-identity credentials (`id_token`), " +
			"since org-scoped credentials cannot create the organization they are scoped to. " +
			"Configurations that run with org-scoped credentials should reference their " +
			"organization via the `tenzir_organization` data source instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the organization.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the organization.",
				Required:    true,
			},
			"icon_url": schema.StringAttribute{
				Description: "URL of the organization icon.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"require_mfa": schema.BoolAttribute{
				Description: "Require multi-factor authentication for all members. " +
					"Can only be enabled on platform deployments with an MFA integration.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
		},
	}
}

func (r *OrganizationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateOrganization(ctx, plan.Name.ValueString(), plan.IconURL.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	// /org/create has no require_mfa parameter; enabling it is a follow-up
	// update. Save state before that call so a failure doesn't orphan the
	// already-created organization.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if plan.RequireMFA.ValueBool() {
		requireMFA := true
		err := r.client.UpdateOrganization(ctx, id, client.OrganizationUpdate{RequireMFA: &requireMFA})
		if err != nil {
			resp.Diagnostics.AddError("Error enabling require_mfa on new organization", err.Error())
			return
		}
	}
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.client.GetOrganization(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization", err.Error())
		return
	}
	if org == nil {
		// Deleted outside of Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(org.Name)
	state.IconURL = types.StringValue(org.IconURL)
	state.RequireMFA = types.BoolValue(org.RequireMFA)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var update client.OrganizationUpdate
	if !plan.Name.Equal(state.Name) {
		update.Name = plan.Name.ValueStringPointer()
	}
	if !plan.IconURL.Equal(state.IconURL) {
		update.IconURL = plan.IconURL.ValueStringPointer()
	}
	if !plan.RequireMFA.Equal(state.RequireMFA) {
		update.RequireMFA = plan.RequireMFA.ValueBoolPointer()
	}
	err := r.client.UpdateOrganization(ctx, plan.ID.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error updating organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteOrganization(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting organization", err.Error())
	}
}

func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// `terraform import tenzir_organization.example <organization-id>`
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
