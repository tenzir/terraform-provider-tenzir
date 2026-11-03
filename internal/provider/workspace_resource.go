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
	_ resource.ResourceWithConfigure   = &workspaceResource{}
	_ resource.ResourceWithImportState = &workspaceResource{}
)

func NewWorkspaceResource() resource.Resource {
	return &workspaceResource{}
}

type workspaceResource struct {
	client *client.Client
}

type workspaceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Slug           types.String `tfsdk:"slug"`
	Name           types.String `tfsdk:"name"`
	IconURL        types.String `tfsdk:"icon_url"`
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A workspace of an organization. It holds deployments, secrets and a git repository " +
			"with their configuration.\n\n" +
			"Needs `Organization.createWorkspace` and `Workspace.read`, `Workspace.update` and `Workspace.delete`. " +
			"The platform refuses to delete a workspace that still holds deployments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The workspace ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization that owns the workspace. An ID survives a slug change.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The workspace's name in URLs: lowercase letters, digits and single dashes.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name. Defaults to the slug.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"icon_url": schema.StringAttribute{
				MarkdownDescription: "The URL of the workspace icon.",
				Optional:            true,
			},
		},
	}
}

func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

// set copies the response, but keeps organization_id as configured, which
// may be a slug.
func (m *workspaceModel) set(ws *client.Workspace) {
	m.ID = types.StringValue(ws.ID)
	m.Slug = types.StringValue(ws.Slug)
	m.Name = types.StringValue(ws.Name)
	m.IconURL = optionalString(ws.IconURL)
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, err := r.client.CreateWorkspace(ctx, plan.OrganizationID.ValueString(), client.NewWorkspace{
		Slug:    plan.Slug.ValueString(),
		Name:    stringPointer(plan.Name),
		IconURL: stringPointer(plan.IconURL),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the workspace", err.Error())
		return
	}
	plan.set(ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, err := r.client.ReadWorkspace(ctx, state.OrganizationID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the workspace", err.Error())
		return
	}
	state.set(ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var patch client.WorkspacePatch
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
	ws, err := r.client.UpdateWorkspace(ctx, state.OrganizationID.ValueString(), state.ID.ValueString(), patch)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the workspace", err.Error())
		return
	}
	plan.set(ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteWorkspace(ctx, state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the workspace", err.Error())
	}
}

// ImportState takes "<organization>/<workspace>", each an ID or a slug, and
// stores IDs.
func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "organization", "workspace")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	ws, err := r.client.ReadWorkspace(ctx, parts[0], parts[1])
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the workspace", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), ws.OrgID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ws.ID)...)
}
