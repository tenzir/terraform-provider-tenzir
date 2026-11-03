package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &rolesDataSource{}

func NewRolesDataSource() datasource.DataSource {
	return &rolesDataSource{}
}

type rolesDataSource struct {
	client *client.Client
}

type rolesModel struct {
	OrganizationID types.String       `tfsdk:"organization_id"`
	Roles          []roleSummaryModel `tfsdk:"roles"`
}

type roleSummaryModel struct {
	ID      types.String `tfsdk:"id"`
	Label   types.String `tfsdk:"label"`
	Color   types.String `tfsdk:"color"`
	Builtin types.Bool   `tfsdk:"builtin"`
}

func (d *rolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The built-in and custom roles of an organization. Needs `Organization.listRoles`.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"roles": schema.ListNestedAttribute{
				MarkdownDescription: "The roles, built-in ones first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":      schema.StringAttribute{Computed: true, MarkdownDescription: "The role ID, such as `org.admin` for a built-in role."},
						"label":   schema.StringAttribute{Computed: true, MarkdownDescription: "The display name."},
						"color":   schema.StringAttribute{Computed: true, MarkdownDescription: "The display color."},
						"builtin": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the platform defines the role."},
					},
				},
			},
		},
	}
}

func (d *rolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *rolesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config rolesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	roles, err := d.client.ListRoles(ctx, config.OrganizationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list the organization's roles", err.Error())
		return
	}
	config.Roles = []roleSummaryModel{}
	for _, r := range roles {
		config.Roles = append(config.Roles, roleSummaryModel{
			ID:      types.StringValue(r.ID),
			Label:   types.StringValue(r.Label),
			Color:   types.StringValue(r.Color),
			Builtin: types.BoolValue(r.Tag == "Builtin"),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
