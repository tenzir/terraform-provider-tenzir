package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &NodeResource{}
var _ resource.ResourceWithImportState = &NodeResource{}

func NewNodeResource() resource.Resource {
	return &NodeResource{}
}

// NodeResource manages the registration of a Tenzir node in a workspace.
type NodeResource struct {
	client *client.Client
}

type NodeResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
	Token       types.String `tfsdk:"token"`
}

func (r *NodeResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_node"
}

func (r *NodeResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A node registration in a Tenzir Platform workspace. The node is " +
			"registered in a disconnected state; obtain a client configuration (e.g. via " +
			"`tenzir-platform tools generate-config`) to connect a running Tenzir node to it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the node.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Description: "Identifier of the workspace the node belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the node.",
				Required:    true,
			},
			"token": schema.StringAttribute{
				Description: "Authentication token the Tenzir node uses to connect to the " +
					"platform, e.g. as the TENZIR_TOKEN environment variable or the " +
					"`platform.tenzir-token` configuration setting. Stored in the Terraform " +
					"state; treat the state as sensitive.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *NodeResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *NodeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan NodeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateNode(ctx, plan.WorkspaceID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating node", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	token, err := r.client.GetNodeToken(ctx, plan.WorkspaceID.ValueString(), id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error fetching token of created node",
			"The node was created with id "+id+" but fetching its token failed, "+
				"so it is not tracked in state. Import it with `terraform import` "+
				"or delete it manually. Error: "+err.Error(),
		)
		return
	}
	plan.Token = types.StringValue(token)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *NodeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state NodeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	node, err := r.client.GetNode(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading node", err.Error())
		return
	}
	if node == nil {
		// The node (or its whole workspace) was deleted outside of
		// Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(node.Name)
	// The token is stable, so refreshing it here detects nothing new for
	// nodes created by Terraform, but fills it in for imported nodes.
	token, err := r.client.GetNodeToken(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error fetching node token", err.Error())
		return
	}
	state.Token = types.StringValue(token)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *NodeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan NodeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RenameNode(ctx,
		plan.WorkspaceID.ValueString(), plan.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error renaming node", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *NodeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state NodeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteNode(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting node", err.Error())
	}
}

func (r *NodeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Nodes are addressed by workspace and node id:
	// `terraform import tenzir_node.example <workspace-id>/<node-id>`
	workspaceID, nodeID, ok := strings.Cut(req.ID, "/")
	if !ok || workspaceID == "" || nodeID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <workspace-id>/<node-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), nodeID)...)
}
