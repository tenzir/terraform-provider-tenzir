package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &apiKeyResource{}
	_ resource.ResourceWithImportState = &apiKeyResource{}
)

func NewAPIKeyResource() resource.Resource {
	return &apiKeyResource{}
}

type apiKeyResource struct {
	client *client.Client
}

type apiKeyModel struct {
	ID        types.String `tfsdk:"id"`
	UserID    types.String `tfsdk:"user_id"`
	Name      types.String `tfsdk:"name"`
	Policy    PolicyValue  `tfsdk:"policy"`
	Key       types.String `tfsdk:"key"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A user API key, presented as the `x-tenzir-api-key` header. Its policy restricts " +
			"the key further; the owner's own permissions still limit every request. Destroying the " +
			"resource revokes the key, and the platform keeps a record of it.\n\n" +
			"Needs `User.createApiKey` on the owner, and `ApiKey.read`, `ApiKey.setPolicy`, `ApiKey.rename` " +
			"and `ApiKey.revoke` on the key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The key ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the user who owns the key. Defaults to the user the provider authenticates as.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The display name. Defaults to none.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"policy": schema.StringAttribute{
				MarkdownDescription: policyDescription("What the key may do,") + " An empty array `[]` allows only account introspection.",
				Required:            true,
				CustomType:          PolicyType{},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The secret key. The platform returns it only when it mints the key, so it is " +
					"null after an import.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the key was minted.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (m *apiKeyModel) set(key *client.APIKey) {
	m.ID = types.StringValue(key.ID)
	m.UserID = types.StringValue(key.Owner.UserID)
	m.Name = types.StringValue(key.Name)
	m.Policy = NewPolicyValue(string(key.Policy))
	m.CreatedAt = types.StringValue(key.CreatedAt)
}

func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	userID := plan.UserID.ValueString()
	if plan.UserID.IsUnknown() || plan.UserID.IsNull() {
		account, err := r.client.ReadAccount(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Cannot tell who the provider authenticates as", err.Error())
			return
		}
		if account.User == nil {
			resp.Diagnostics.AddError("No user to own the key",
				"The provider authenticates as a "+account.Tag+", not as a user; set `user_id`.")
			return
		}
		userID = account.User.ID
	}
	minted, err := r.client.CreateAPIKey(ctx, userID, plan.Name.ValueString(), json.RawMessage(plan.Policy.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the API key", err.Error())
		return
	}
	plan.set(&minted.APIKey)
	plan.Key = types.StringValue(minted.Key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.ReadAPIKey(ctx, state.UserID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the API key", err.Error())
		return
	}
	if key.RevokedAt != nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.set(key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *apiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state apiKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	user, id := state.UserID.ValueString(), state.ID.ValueString()
	key := &client.APIKey{}
	var err error
	if !plan.Name.Equal(state.Name) {
		if key, err = r.client.RenameAPIKey(ctx, user, id, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("Cannot rename the API key", err.Error())
			return
		}
	}
	if !plan.Policy.Equal(state.Policy) {
		if key, err = r.client.SetAPIKeyPolicy(ctx, user, id, json.RawMessage(plan.Policy.ValueString())); err != nil {
			resp.Diagnostics.AddError("Cannot change the API key's policy", err.Error())
			return
		}
	}
	if key.ID == "" {
		if key, err = r.client.ReadAPIKey(ctx, user, id); err != nil {
			resp.Diagnostics.AddError("Cannot read the API key", err.Error())
			return
		}
	}
	plan.set(key)
	plan.Key = state.Key
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.RevokeAPIKey(ctx, state.UserID.ValueString(), state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot revoke the API key", err.Error())
	}
}

// ImportState takes "<user>/<key>", because a key is named under its owner.
func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "user", "key")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
