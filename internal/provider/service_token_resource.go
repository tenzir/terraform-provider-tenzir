package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &ServiceTokenResource{}
var _ resource.ResourceWithImportState = &ServiceTokenResource{}

func NewServiceTokenResource() resource.Resource {
	return &ServiceTokenResource{}
}

// ServiceTokenResource manages an organization-level service token, a static
// machine credential acting as a synthetic organization member.
type ServiceTokenResource struct {
	client *client.Client
}

type ServiceTokenResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	Label            types.String `tfsdk:"label"`
	Role             types.String `tfsdk:"role"`
	ExpiresInSeconds types.Int64  `tfsdk:"expires_in_seconds"`
	Token            types.String `tfsdk:"token"`
}

func (r *ServiceTokenResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_token"
}

func (r *ServiceTokenResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An organization-level service token: a static machine credential " +
			"(`tzst_...`) that acts inside its organization as a synthetic member with the " +
			"configured role, e.g. as the `service_token` credential of this provider. " +
			"Requires org-admin credentials.\n\n" +
			"The platform has no update operation for service tokens, so changing any " +
			"attribute replaces (revokes and re-creates) the token. Expired or revoked " +
			"tokens are planned for re-creation on the next refresh.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the token (not the secret).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Description: "Identifier of the organization the token belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				Description: "Human-readable label of the token.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Description: "Organization role of the token's service identity, `member` or " +
					"`admin`. Defaults to `member`.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("member"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"expires_in_seconds": schema.Int64Attribute{
				Description: "Lifetime of the token in seconds, counted from its creation. " +
					"Unset means the token never expires. Only used at creation time; the " +
					"platform does not return it, so it stays unset for imported tokens.",
				Optional: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"token": schema.StringAttribute{
				Description: "The plaintext token secret (`tzst_...`). The platform reveals it " +
					"exactly once at creation time and only persists its hash, so it stays " +
					"unset for imported tokens. Stored in the Terraform state; treat the " +
					"state as sensitive.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ServiceTokenResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *ServiceTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ServiceTokenResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var expiresInSeconds *int64
	if !plan.ExpiresInSeconds.IsNull() {
		v := plan.ExpiresInSeconds.ValueInt64()
		expiresInSeconds = &v
	}
	id, secret, err := r.client.CreateServiceToken(ctx,
		plan.OrganizationID.ValueString(),
		plan.Label.ValueString(),
		plan.Role.ValueString(),
		expiresInSeconds)
	if err != nil {
		resp.Diagnostics.AddError("Error creating service token", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	plan.Token = types.StringValue(secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServiceTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ServiceTokenResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	token, err := r.client.GetServiceToken(ctx,
		state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading service token", err.Error())
		return
	}
	if token == nil {
		// The token was revoked (or its whole organization deleted) outside
		// of Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}
	if token.ExpiresAt != nil {
		// An expired token can no longer authenticate, so treat it like a
		// revoked one and plan a re-create.
		if expiry, err := time.Parse(time.RFC3339, *token.ExpiresAt); err == nil && time.Now().After(expiry) {
			resp.State.RemoveResource(ctx)
			return
		}
	}

	state.Label = types.StringValue(token.Label)
	state.Role = types.StringValue(token.Role)
	// The plaintext secret (`token`) is only known at creation time and never
	// returned by the platform, so keep whatever the state has: the created
	// secret, or unset for imported tokens. `expires_in_seconds` is likewise
	// a create-only input that cannot be refreshed.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ServiceTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Unreachable: every attribute carries a RequiresReplace plan modifier,
	// since the platform has no update endpoint for service tokens.
	resp.Diagnostics.AddError(
		"Service tokens cannot be updated in place",
		"This is a bug in the provider: all attribute changes should have planned a replacement.",
	)
}

func (r *ServiceTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ServiceTokenResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RevokeServiceToken(ctx,
		state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error revoking service token", err.Error())
	}
}

func (r *ServiceTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Service tokens are addressed by organization and token id:
	// `terraform import tenzir_service_token.example <organization-id>/<token-id>`
	// The `token` attribute stays unset: the platform reveals the secret only
	// at creation time.
	organizationID, tokenID, ok := strings.Cut(req.ID, "/")
	if !ok || organizationID == "" || tokenID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <organization-id>/<token-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), organizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), tokenID)...)
}
