package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &peopleDataSource{}

func NewPeopleDataSource() datasource.DataSource {
	return &peopleDataSource{}
}

type peopleDataSource struct {
	client *client.Client
}

type peopleModel struct {
	OrganizationID types.String  `tfsdk:"organization_id"`
	People         []personModel `tfsdk:"people"`
}

type personModel struct {
	UserID  types.String   `tfsdk:"user_id"`
	Name    types.String   `tfsdk:"name"`
	Email   types.String   `tfsdk:"email"`
	RoleIDs []types.String `tfsdk:"role_ids"`
}

func (d *peopleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_people"
}

func (d *peopleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The members of an organization and their roles. Needs `Organization.listPeople`.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"people": schema.ListNestedAttribute{
				MarkdownDescription: "The members.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"user_id":  schema.StringAttribute{Computed: true, MarkdownDescription: "The user ID."},
						"name":     schema.StringAttribute{Computed: true, MarkdownDescription: "The name, if known."},
						"email":    schema.StringAttribute{Computed: true, MarkdownDescription: "The email address, if known."},
						"role_ids": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The IDs of the assigned roles."},
					},
				},
			},
		},
	}
}

func (d *peopleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *peopleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config peopleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	people, err := d.client.ListPeople(ctx, config.OrganizationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list the organization's members", err.Error())
		return
	}
	config.People = []personModel{}
	for _, p := range people {
		roles := []types.String{}
		for _, r := range p.Roles {
			roles = append(roles, types.StringValue(r.ID))
		}
		config.People = append(config.People, personModel{
			UserID:  types.StringValue(p.UserID),
			Name:    optionalString(p.Name),
			Email:   optionalString(p.Email),
			RoleIDs: roles,
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
