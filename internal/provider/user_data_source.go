package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &userDataSource{}

func NewUserDataSource() datasource.DataSource {
	return &userDataSource{}
}

type userDataSource struct {
	client *client.Client
}

type userModel struct {
	Email types.String `tfsdk:"email"`
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A user, looked up by email address, such as to add them to an organization with " +
			"`tenzir_organization_member`. Fails unless exactly one active user has the address.\n\n" +
			"Needs `Root.listUsers`, which only platform administrators hold.",
		Attributes: map[string]schema.Attribute{
			"email": schema.StringAttribute{
				MarkdownDescription: "The email address, in any letter case.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The user ID.",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name, if known.",
				Computed:            true,
			},
		},
	}
}

func (d *userDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	users, err := d.client.ListUsersByEmail(ctx, config.Email.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot look up the user", err.Error())
		return
	}
	if len(users) != 1 {
		resp.Diagnostics.AddError("Cannot look up the user",
			fmt.Sprintf("%d active users have the email address %s, expected exactly one", len(users), config.Email.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &userModel{
		Email: config.Email,
		ID:    types.StringValue(users[0].ID),
		Name:  optionalString(users[0].Name),
	})...)
}
