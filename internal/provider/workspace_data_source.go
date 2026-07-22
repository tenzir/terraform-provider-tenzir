package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSource = &WorkspaceDataSource{}

func NewWorkspaceDataSource() datasource.DataSource {
	return &WorkspaceDataSource{}
}

// WorkspaceDataSource looks up an existing workspace by id.
type WorkspaceDataSource struct {
	client *client.Client
}

type WorkspaceDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	IconURL        types.String `tfsdk:"icon_url"`
	OwnerNamespace types.String `tfsdk:"owner_namespace"`
	OwnerID        types.String `tfsdk:"owner_id"`
}

func (d *WorkspaceDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *WorkspaceDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing workspace in the Tenzir Platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the workspace.",
				Required:    true,
			},
			"name": schema.StringAttribute{
				Description: "Display name of the workspace.",
				Computed:    true,
			},
			"icon_url": schema.StringAttribute{
				Description: "URL of the workspace icon.",
				Computed:    true,
			},
			"owner_namespace": schema.StringAttribute{
				Description: "Namespace of the workspace owner (e.g. `organization`).",
				Computed:    true,
			},
			"owner_id": schema.StringAttribute{
				Description: "Identifier of the workspace owner.",
				Computed:    true,
			},
		},
	}
}

func (d *WorkspaceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (d *WorkspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config WorkspaceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws, err := d.client.GetWorkspace(ctx, config.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace", err.Error())
		return
	}
	if ws == nil {
		resp.Diagnostics.AddError(
			"Workspace not found",
			fmt.Sprintf("No workspace with id %q exists.", config.ID.ValueString()),
		)
		return
	}

	config.Name = types.StringValue(ws.Name)
	config.IconURL = types.StringValue(ws.IconURL)
	config.OwnerNamespace = types.StringValue(ws.Owner.Namespace)
	config.OwnerID = types.StringValue(ws.Owner.OwnerID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
