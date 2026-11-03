package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &organizationDataSource{}

func NewOrganizationDataSource() datasource.DataSource {
	return &organizationDataSource{}
}

type organizationDataSource struct {
	client *client.Client
}

type organizationModel struct {
	ID          types.String `tfsdk:"id"`
	Slug        types.String `tfsdk:"slug"`
	Name        types.String `tfsdk:"name"`
	IconURL     types.String `tfsdk:"icon_url"`
	MemberCount types.Int64  `tfsdk:"member_count"`
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An organization, looked up by ID or slug. Needs `Organization.read`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The organization ID. Set this or `slug`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug")),
				},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The organization's name in URLs. Set this or `id`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name.",
				Computed:            true,
			},
			"icon_url": schema.StringAttribute{
				MarkdownDescription: "The URL of the organization icon.",
				Computed:            true,
			},
			"member_count": schema.Int64Attribute{
				MarkdownDescription: "How many users are members.",
				Computed:            true,
			},
		},
	}
}

func (d *organizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *organizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config organizationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ref := config.ID.ValueString()
	if ref == "" {
		ref = config.Slug.ValueString()
	}
	org, err := d.client.ReadOrganization(ctx, ref)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &organizationModel{
		ID:          types.StringValue(org.ID),
		Slug:        optionalString(org.Slug),
		Name:        types.StringValue(org.Name),
		IconURL:     optionalString(org.IconURL),
		MemberCount: types.Int64Value(org.MemberCount),
	})...)
}
