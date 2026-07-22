package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &WorkspaceResource{}
var _ resource.ResourceWithImportState = &WorkspaceResource{}

func NewWorkspaceResource() resource.Resource {
	return &WorkspaceResource{}
}

// WorkspaceResource manages a Tenzir Platform workspace.
type WorkspaceResource struct {
	client *client.Client
}

type WorkspaceResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	IconURL        types.String `tfsdk:"icon_url"`
	OwnerNamespace types.String `tfsdk:"owner_namespace"`
	OwnerID        types.String `tfsdk:"owner_id"`
}

func (r *WorkspaceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *WorkspaceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A workspace in the Tenzir Platform. Workspaces created by this resource " +
			"are owned by the organization of the user authenticated by the provider, so that " +
			"organization must exist first — reference it with `depends_on` when managing both " +
			"in the same configuration. (Personal workspaces are not supported because the API " +
			"does not allow deleting them.)",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the workspace.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the workspace.",
				Required:    true,
			},
			"icon_url": schema.StringAttribute{
				Description: "URL of the workspace icon.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
			},
			"owner_namespace": schema.StringAttribute{
				Description: "Namespace of the workspace owner (e.g. `organization`).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"owner_id": schema.StringAttribute{
				Description: "Identifier of the workspace owner.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *WorkspaceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *WorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateWorkspace(ctx, plan.Name.ValueString(), plan.IconURL.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	// Read back to fill the computed owner attributes.
	ws, err := r.client.GetWorkspace(ctx, id)
	if err == nil && ws == nil {
		err = errors.New("workspace disappeared while creating it")
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading back created workspace",
			"The workspace was created with id "+id+" but reading it back failed, "+
				"so it is not tracked in state. Import it with `terraform import` "+
				"or delete it manually. Error: "+err.Error(),
		)
		return
	}
	plan.OwnerNamespace = types.StringValue(ws.Owner.Namespace)
	plan.OwnerID = types.StringValue(ws.Owner.OwnerID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws, err := r.client.GetWorkspace(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace", err.Error())
		return
	}
	if ws == nil {
		// Deleted outside of Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(ws.Name)
	state.IconURL = types.StringValue(ws.IconURL)
	state.OwnerNamespace = types.StringValue(ws.Owner.Namespace)
	state.OwnerID = types.StringValue(ws.Owner.OwnerID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.ID.ValueString()
	if !plan.Name.Equal(state.Name) {
		if err := r.client.RenameWorkspace(ctx, id, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("Error renaming workspace", err.Error())
			return
		}
	}
	if !plan.IconURL.Equal(state.IconURL) {
		if err := r.client.UpdateWorkspaceIcon(ctx, id, plan.IconURL.ValueString()); err != nil {
			resp.Diagnostics.AddError("Error updating workspace icon", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteWorkspace(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting workspace", err.Error())
	}
}

func (r *WorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// `terraform import tenzir_workspace.example <workspace-id>`
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
