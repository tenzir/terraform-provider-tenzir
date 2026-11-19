package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &organizationResource{}
	_ resource.ResourceWithImportState = &organizationResource{}
)

// provisionedPlan is the plan an administrator sets an organization up on.
const provisionedPlan = "provisioned"

func NewOrganizationResource() resource.Resource {
	return &organizationResource{}
}

type organizationResource struct {
	client *client.Client
}

type organizationResourceModel struct {
	ID      types.String `tfsdk:"id"`
	Slug    types.String `tfsdk:"slug"`
	Name    types.String `tfsdk:"name"`
	IconURL types.String `tfsdk:"icon_url"`
	Plan    types.String `tfsdk:"plan"`
}

func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An organization.\n\n" +
			"Without `plan`, the provider's authenticated user becomes the sole owner of a new Community organization; " +
			"this needs `User.createOrganization` on that user. Terraform leaves subsequent plan changes to its owners.\n\n" +
			"With `plan = \"provisioned\"`, a platform administrator sets the organization up, such as Tenzir's " +
			"demo organization: the platform creates it on the provisioned plan, which has everything Enterprise has and bills nothing, and which nobody in the " +
			"organization can change. It has no members; add the account that manages it as its owner with " +
			"`tenzir_organization_member`. This needs `Root.createOrganization`, which only platform " +
			"administrators hold.\n\n" +
			"Changing it needs `Organization.update`, and deleting it `Organization.delete`. The platform refuses " +
			"to delete an organization that still holds workspaces or ingest endpoints, and refuses to move the " +
			"slug of an organization on Community.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The organization ID, which the platform picks.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The organization's name in URLs: lowercase letters, digits and single dashes, " +
					"unique across the platform.",
				Required: true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name. Defaults to the slug.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"icon_url": schema.StringAttribute{
				MarkdownDescription: "The URL of the organization icon.",
				Optional:            true,
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "`provisioned` to set the organization up as a platform administrator; the " +
					"only value. Unset, Terraform does not manage the plan. The platform cannot move an organization " +
					"onto or off the provisioned plan, so changing this replaces the organization.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf(provisionedPlan),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *organizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (m *organizationResourceModel) set(org *client.Organization) {
	m.ID = types.StringValue(org.ID)
	m.Slug = optionalString(org.Slug)
	m.Name = types.StringValue(org.Name)
	m.IconURL = optionalString(org.IconURL)
}

func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := client.NewOrganization{
		Slug:    plan.Slug.ValueString(),
		Name:    stringPointer(plan.Name),
		IconURL: stringPointer(plan.IconURL),
	}
	var org *client.Organization
	var err error
	if plan.Plan.ValueString() == provisionedPlan {
		org, err = r.client.ProvisionOrganization(ctx, body)
	} else {
		account, accountErr := r.client.ReadAccount(ctx)
		if accountErr != nil {
			resp.Diagnostics.AddError("Cannot tell who the provider authenticates as", accountErr.Error())
			return
		}
		if account.User == nil {
			resp.Diagnostics.AddError("No user to own the organization",
				"The provider authenticates as a "+account.Tag+", not as a user; creating an organization without `plan` needs a user credential.")
			return
		}
		org, err = r.client.CreateOrganization(ctx, account.User.ID, body)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the organization", err.Error())
		return
	}
	plan.set(org)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *organizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	org, err := r.client.ReadOrganization(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization", err.Error())
		return
	}
	state.set(org)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var patch client.OrganizationPatch
	if !plan.Slug.Equal(state.Slug) {
		patch.Slug = stringPointer(plan.Slug)
	}
	if !plan.Name.Equal(state.Name) {
		patch.Name = stringPointer(plan.Name)
	}
	if !plan.IconURL.Equal(state.IconURL) {
		patch.IconURL = stringPointer(plan.IconURL)
		patch.ClearIconURL = plan.IconURL.IsNull()
	}
	org, err := r.client.UpdateOrganization(ctx, state.ID.ValueString(), patch)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the organization", err.Error())
		return
	}
	plan.set(org)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *organizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteOrganization(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the organization", err.Error())
	}
}

// ImportState takes an ID or a slug, and stores the ID. Read leaves the plan
// as configured, because a Sovereign installation reports every organization
// as sovereign, so only an import learns it from the platform.
func (r *organizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, err := r.client.ReadOrganization(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), org.ID)...)
	if org.Edition == provisionedPlan {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("plan"), provisionedPlan)...)
	}
}
