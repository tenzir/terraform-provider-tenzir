package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSource = &OrganizationMembersDataSource{}

func NewOrganizationMembersDataSource() datasource.DataSource {
	return &OrganizationMembersDataSource{}
}

// OrganizationMembersDataSource lists all members of an organization,
// including members that are managed outside of Terraform (e.g. provisioned
// via invitations or federation).
type OrganizationMembersDataSource struct {
	client *client.Client
}

type OrganizationMembersDataSourceModel struct {
	OrganizationID types.String                         `tfsdk:"organization_id"`
	Members        []OrganizationMembersDataMemberModel `tfsdk:"members"`
}

type OrganizationMembersDataMemberModel struct {
	UserID types.String `tfsdk:"user_id"`
	Role   types.String `tfsdk:"role"`
	Email  types.String `tfsdk:"email"`
	Name   types.String `tfsdk:"name"`
}

func (d *OrganizationMembersDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_members"
}

func (d *OrganizationMembersDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List all members of a Tenzir Platform organization, including " +
			"members managed outside of Terraform (e.g. provisioned via invitations " +
			"or workload identity federation).",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{
				Description: "Identifier of the organization whose members are listed.",
				Required:    true,
			},
			"members": schema.ListNestedAttribute{
				Description: "All members of the organization.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"user_id": schema.StringAttribute{
							Description: "Unique identifier of the user.",
							Computed:    true,
						},
						"role": schema.StringAttribute{
							Description: "Role of the member in the organization, `member` or `admin`.",
							Computed:    true,
						},
						"email": schema.StringAttribute{
							Description: "Email address from the user's profile. Null until the " +
								"user has logged in at least once.",
							Computed: true,
						},
						"name": schema.StringAttribute{
							Description: "Display name from the user's profile. Null until the " +
								"user has logged in at least once.",
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *OrganizationMembersDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (d *OrganizationMembersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config OrganizationMembersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	members, err := d.client.ListOrganizationMembers(ctx, config.OrganizationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error listing organization members", err.Error())
		return
	}

	config.Members = make([]OrganizationMembersDataMemberModel, 0, len(members))
	for _, m := range members {
		config.Members = append(config.Members, OrganizationMembersDataMemberModel{
			UserID: types.StringValue(m.UserID),
			Role:   types.StringValue(m.Role),
			Email:  types.StringPointerValue(m.Email),
			Name:   types.StringPointerValue(m.Name),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
