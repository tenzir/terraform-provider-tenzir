package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSource = &OrganizationDataSource{}

func NewOrganizationDataSource() datasource.DataSource {
	return &OrganizationDataSource{}
}

// OrganizationDataSource looks up an existing organization by id. This is
// the typical way to reference the organization in configurations that use
// org-scoped credentials, which cannot create organizations themselves.
type OrganizationDataSource struct {
	client *client.Client
}

type OrganizationDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	IconURL    types.String `tfsdk:"icon_url"`
	RequireMFA types.Bool   `tfsdk:"require_mfa"`
}

func (d *OrganizationDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *OrganizationDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing organization in the Tenzir Platform. Use this to " +
			"reference an organization that is managed outside of Terraform, e.g. when the " +
			"provider runs with credentials scoped to that organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the organization.",
				Required:    true,
			},
			"name": schema.StringAttribute{
				Description: "Display name of the organization.",
				Computed:    true,
			},
			"icon_url": schema.StringAttribute{
				Description: "URL of the organization icon.",
				Computed:    true,
			},
			"require_mfa": schema.BoolAttribute{
				Description: "Whether multi-factor authentication is required for all members.",
				Computed:    true,
			},
		},
	}
}

func (d *OrganizationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (d *OrganizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config OrganizationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := d.client.GetOrganization(ctx, config.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization", err.Error())
		return
	}
	if org == nil {
		resp.Diagnostics.AddError(
			"Organization not found",
			fmt.Sprintf("No organization with id %q exists.", config.ID.ValueString()),
		)
		return
	}

	config.Name = types.StringValue(org.Name)
	config.IconURL = types.StringValue(org.IconURL)
	config.RequireMFA = types.BoolValue(org.RequireMFA)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
