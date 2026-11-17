package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &currentUserDataSource{}

func NewCurrentUserDataSource() datasource.DataSource {
	return &currentUserDataSource{}
}

type currentUserDataSource struct {
	client *client.Client
}

type currentUserModel struct {
	ID    types.String `tfsdk:"id"`
	Email types.String `tfsdk:"email"`
	Name  types.String `tfsdk:"name"`
}

func (d *currentUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_current_user"
}

func (d *currentUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The user that the provider's credential acts as, such as to make the account " +
			"that manages an organization its owner with `tenzir_organization_member`. Needs no permission; fails " +
			"for a credential that is no user's.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The user ID.",
				Computed:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The email address, if known.",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name, if known.",
				Computed:            true,
			},
		},
	}
}

func (d *currentUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *currentUserDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	principal, err := d.client.ReadAccount(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the current user", err.Error())
		return
	}
	if principal.User == nil {
		resp.Diagnostics.AddError("Cannot read the current user",
			"The provider's credential authenticates "+principal.Tag+", which is no user.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &currentUserModel{
		ID:    types.StringValue(principal.User.ID),
		Email: types.StringPointerValue(principal.User.Email),
		Name:  types.StringPointerValue(principal.User.Name),
	})...)
}
