package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &demoOrganizationResource{}
	_ resource.ResourceWithImportState = &demoOrganizationResource{}
)

func NewDemoOrganizationResource() resource.Resource {
	return &demoOrganizationResource{}
}

type demoOrganizationResource struct {
	client *client.Client
}

type demoOrganizationModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	RoleID         types.String `tfsdk:"role_id"`
}

func (r *demoOrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_demo_organization"
}

func (r *demoOrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Tenzir-internal: the organization every new user of the platform joins on signing " +
			"up, to look around in, and the custom role they get there, which decides what they may see. A " +
			"platform has at most one, so declare this resource once; a second one replaces the first's " +
			"setting. Members who joined before stay when it changes or goes.\n\n" +
			"Needs `Root.readDemoOrgSetup`, `Root.setDemoOrgSetup` and `Root.removeDemoOrgSetup`, which only " +
			"platform administrators hold. Deleting the organization or the role makes the platform forget the " +
			"setting, which the next plan shows.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the demo organization.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the organization; not its slug.",
				Required:            true,
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "The ID of a custom role of the organization, which new users get there.",
				Required:            true,
			},
		},
	}
}

func (r *demoOrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (m *demoOrganizationModel) set(setup *client.DemoOrgSetup) {
	m.ID = types.StringValue(setup.OrganizationID)
	m.OrganizationID = types.StringValue(setup.OrganizationID)
	m.RoleID = types.StringValue(setup.RoleID)
}

func (r *demoOrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan demoOrganizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	setup, err := r.client.SetDemoOrgSetup(ctx, client.DemoOrgSetup{
		OrganizationID: plan.OrganizationID.ValueString(),
		RoleID:         plan.RoleID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot set the demo organization", err.Error())
		return
	}
	plan.set(setup)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *demoOrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state demoOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	setup, err := r.client.ReadDemoOrgSetup(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the demo organization", err.Error())
		return
	}
	if setup == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.set(setup)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *demoOrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan demoOrganizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	setup, err := r.client.SetDemoOrgSetup(ctx, client.DemoOrgSetup{
		OrganizationID: plan.OrganizationID.ValueString(),
		RoleID:         plan.RoleID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot set the demo organization", err.Error())
		return
	}
	plan.set(setup)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *demoOrganizationResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.RemoveDemoOrgSetup(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot remove the demo organization", err.Error())
	}
}

// ImportState takes any ID, because the platform has at most one demo
// organization, and reads it.
func (r *demoOrganizationResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setup, err := r.client.ReadDemoOrgSetup(ctx)
	if err == nil && setup == nil {
		resp.Diagnostics.AddError("Cannot import the demo organization", "The platform has none.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the demo organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), setup.OrganizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), setup.OrganizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role_id"), setup.RoleID)...)
}
