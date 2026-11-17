package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &membersDataSource{}

func NewMembersDataSource() datasource.DataSource {
	return &membersDataSource{}
}

type membersDataSource struct {
	client *client.Client
}

type membersModel struct {
	OrganizationID types.String  `tfsdk:"organization_id"`
	Members        []memberModel `tfsdk:"members"`
}

type memberModel struct {
	ID              types.String   `tfsdk:"id"`
	UserID          types.String   `tfsdk:"user_id"`
	Name            types.String   `tfsdk:"name"`
	Email           types.String   `tfsdk:"email"`
	InvitationLabel types.String   `tfsdk:"invitation_label"`
	RoleIDs         []types.String `tfsdk:"role_ids"`
}

func (d *membersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_members"
}

func (d *membersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The members of an organization and their roles, pending invitations included. " +
			"Needs `Organization.listMembers`.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"members": schema.ListNestedAttribute{
				MarkdownDescription: "The members.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":               schema.StringAttribute{Computed: true, MarkdownDescription: "The member ID, which `tenzir_role_assignment` takes."},
						"user_id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The user ID, unless the member is an invitation nobody redeemed yet."},
						"name":             schema.StringAttribute{Computed: true, MarkdownDescription: "The user's name, if known."},
						"email":            schema.StringAttribute{Computed: true, MarkdownDescription: "The user's email address, if known."},
						"invitation_label": schema.StringAttribute{Computed: true, MarkdownDescription: "The label of the member's invitation, if it joined by one."},
						"role_ids":         schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The IDs of the assigned roles."},
					},
				},
			},
		},
	}
}

func (d *membersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *membersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config membersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	members, err := d.client.ListMembers(ctx, config.OrganizationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list the organization's members", err.Error())
		return
	}
	config.Members = []memberModel{}
	for _, m := range members {
		roles := []types.String{}
		for _, r := range m.Roles {
			roles = append(roles, types.StringValue(r.ID))
		}
		member := memberModel{
			ID:              types.StringValue(m.ID),
			UserID:          types.StringNull(),
			Name:            types.StringNull(),
			Email:           types.StringNull(),
			InvitationLabel: types.StringNull(),
			RoleIDs:         roles,
		}
		if m.User != nil {
			member.UserID = types.StringValue(m.User.ID)
			member.Name = optionalString(m.User.Name)
			member.Email = optionalString(m.User.Email)
		}
		if m.Invitation != nil {
			member.InvitationLabel = types.StringValue(m.Invitation.Label)
		}
		config.Members = append(config.Members, member)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
