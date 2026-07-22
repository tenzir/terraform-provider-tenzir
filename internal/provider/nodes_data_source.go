package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSource = &NodesDataSource{}

func NewNodesDataSource() datasource.DataSource {
	return &NodesDataSource{}
}

// NodesDataSource lists all nodes of a workspace, including nodes that are
// managed outside of Terraform (e.g. demo nodes or nodes created via the
// app).
type NodesDataSource struct {
	client *client.Client
}

type NodesDataSourceModel struct {
	WorkspaceID types.String         `tfsdk:"workspace_id"`
	Nodes       []NodesDataNodeModel `tfsdk:"nodes"`
}

type NodesDataNodeModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Demo      types.Bool   `tfsdk:"demo"`
	Ephemeral types.Bool   `tfsdk:"ephemeral"`
}

func (d *NodesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_nodes"
}

func (d *NodesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List all nodes of a Tenzir Platform workspace, including nodes " +
			"managed outside of Terraform. Returns stored node metadata without " +
			"performing a live connectivity check.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Description: "Identifier of the workspace whose nodes are listed.",
				Required:    true,
			},
			"nodes": schema.ListNestedAttribute{
				Description: "All nodes of the workspace.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Unique identifier of the node.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Display name of the node.",
							Computed:    true,
						},
						"demo": schema.BoolAttribute{
							Description: "Whether the node was created as a demo node.",
							Computed:    true,
						},
						"ephemeral": schema.BoolAttribute{
							Description: "Whether the node was created as an ephemeral node.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *NodesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (d *NodesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config NodesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	nodes, err := d.client.ListNodes(ctx, config.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error listing nodes", err.Error())
		return
	}

	config.Nodes = make([]NodesDataNodeModel, 0, len(nodes))
	for _, n := range nodes {
		config.Nodes = append(config.Nodes, NodesDataNodeModel{
			ID:        types.StringValue(n.ID),
			Name:      types.StringValue(n.Name),
			Demo:      types.BoolValue(n.Demo),
			Ephemeral: types.BoolValue(n.Ephemeral),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
