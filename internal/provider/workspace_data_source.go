package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &workspaceDataSource{}

func NewWorkspaceDataSource() datasource.DataSource {
	return &workspaceDataSource{}
}

type workspaceDataSource struct {
	client *client.Client
}

func (d *workspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *workspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A workspace, looked up by ID or slug. Needs `Workspace.read`.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization that owns the workspace.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The workspace ID. Set this or `slug`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug")),
				},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The workspace's name in URLs. Set this or `id`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name.",
				Computed:            true,
			},
			"icon_url": schema.StringAttribute{
				MarkdownDescription: "The URL of the workspace icon.",
				Computed:            true,
			},
		},
	}
}

func (d *workspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *workspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config workspaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ref := config.ID.ValueString()
	if ref == "" {
		ref = config.Slug.ValueString()
	}
	ws, err := d.client.ReadWorkspace(ctx, config.OrganizationID.ValueString(), ref)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the workspace", err.Error())
		return
	}
	config.set(ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
