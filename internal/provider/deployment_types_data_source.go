package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &deploymentTypesDataSource{}

func NewDeploymentTypesDataSource() datasource.DataSource {
	return &deploymentTypesDataSource{}
}

type deploymentTypesDataSource struct {
	client *client.Client
}

type deploymentTypesModel struct {
	OrganizationID types.String          `tfsdk:"organization_id"`
	WorkspaceID    types.String          `tfsdk:"workspace_id"`
	Edition        types.String          `tfsdk:"edition"`
	Availability   types.String          `tfsdk:"availability"`
	Regions        []types.String        `tfsdk:"regions"`
	DefaultSize    types.String          `tfsdk:"default_size"`
	Types          []deploymentTypeModel `tfsdk:"types"`
}

type deploymentTypeModel struct {
	Size           types.String `tfsdk:"size"`
	CreditsPerHour types.String `tfsdk:"credits_per_hour"`
}

func (d *deploymentTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment_types"
}

func (d *deploymentTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The sizes and regions a deployment that Tenzir hosts can have in a workspace, " +
			"and what each size costs. Needs `Workspace.listDeploymentTypes`.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{MarkdownDescription: "The ID or slug of the organization.", Required: true},
			"workspace_id":    schema.StringAttribute{MarkdownDescription: "The ID or slug of the workspace.", Required: true},
			"edition":         schema.StringAttribute{MarkdownDescription: "The organization's edition: `community`, `enterprise` or `sovereign`.", Computed: true},
			"availability": schema.StringAttribute{
				MarkdownDescription: "Whether the organization can have hosted deployments: `available`, `requiresEnterprise` or `unavailable`.",
				Computed:            true,
			},
			"regions":      schema.ListAttribute{MarkdownDescription: "The cloud regions on offer, the default first.", Computed: true, ElementType: types.StringType},
			"default_size": schema.StringAttribute{MarkdownDescription: "The size a deployment gets when none is given.", Computed: true},
			"types": schema.ListNestedAttribute{
				MarkdownDescription: "The sizes on offer, from smallest to largest.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"size": schema.StringAttribute{MarkdownDescription: "The size, such as `small`.", Computed: true},
						"credits_per_hour": schema.StringAttribute{
							MarkdownDescription: "What a running hour costs, as a decimal; null where usage has no price.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *deploymentTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *deploymentTypesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config deploymentTypesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	offer, err := d.client.ListDeploymentTypes(ctx, config.OrganizationID.ValueString(), config.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list the deployment types", err.Error())
		return
	}
	config.Edition = types.StringValue(offer.Edition)
	config.Availability = types.StringValue(offer.Availability)
	config.DefaultSize = types.StringValue(offer.DefaultSize)
	config.Regions = []types.String{}
	for _, r := range offer.Regions {
		config.Regions = append(config.Regions, types.StringValue(r))
	}
	config.Types = []deploymentTypeModel{}
	for _, t := range offer.Types {
		rate := types.StringNull()
		if raw := strings.Trim(string(t.CreditsPerHour), `"`); raw != "" && raw != "null" {
			rate = types.StringValue(raw)
		}
		config.Types = append(config.Types, deploymentTypeModel{Size: types.StringValue(t.Size), CreditsPerHour: rate})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
