package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &FederatedCredentialResource{}
var _ resource.ResourceWithImportState = &FederatedCredentialResource{}

func NewFederatedCredentialResource() resource.Resource {
	return &FederatedCredentialResource{}
}

// FederatedCredentialResource manages an organization-level OIDC workload
// identity federation credential.
type FederatedCredentialResource struct {
	client *client.Client
}

type FederatedCredentialResourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationID  types.String `tfsdk:"organization_id"`
	Label           types.String `tfsdk:"label"`
	Issuer          types.String `tfsdk:"issuer"`
	Audience        types.String `tfsdk:"audience"`
	ClaimConditions types.Map    `tfsdk:"claim_conditions"`
	Role            types.String `tfsdk:"role"`
}

func (r *FederatedCredentialResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federated_credential"
}

func (r *FederatedCredentialResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An organization-level OIDC workload identity federation credential. " +
			"External OIDC tokens (e.g. issued by a CI system) whose issuer, audience, and " +
			"all claim conditions match can be exchanged for Tenzir Platform keys and act " +
			"as an organization member with the configured role. Requires org-admin " +
			"credentials.\n\n" +
			"The platform has no update operation for federated credentials, so changing " +
			"any attribute replaces the credential. Already-minted keys stay valid until " +
			"they expire.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the credential.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Description: "Identifier of the organization the credential belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				Description: "Human-readable label of the credential.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"issuer": schema.StringAttribute{
				Description: "OIDC issuer URL, exactly as it appears in the tokens' `iss` claim.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"audience": schema.StringAttribute{
				Description: "Required `aud` claim value of presented tokens.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"claim_conditions": schema.MapAttribute{
				Description: "Map of claim name to required exact string value; ALL " +
					"conditions must match. Must contain at least one condition, since " +
					"trusting an entire issuer is not supported.",
				ElementType: types.StringType,
				Required:    true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Description: "Organization role of the federated identity, `member` or `admin`. " +
					"Defaults to `member`.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("member"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *FederatedCredentialResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *FederatedCredentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var claimConditions map[string]string
	resp.Diagnostics.Append(plan.ClaimConditions.ElementsAs(ctx, &claimConditions, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.AddFederatedCredential(ctx, plan.OrganizationID.ValueString(), client.FederatedCredential{
		Label:           plan.Label.ValueString(),
		Issuer:          plan.Issuer.ValueString(),
		Audience:        plan.Audience.ValueString(),
		ClaimConditions: claimConditions,
		Role:            plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cred, err := r.client.GetFederatedCredential(ctx,
		state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading federated credential", err.Error())
		return
	}
	if cred == nil {
		// The credential (or its whole organization) was deleted outside of
		// Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Label = types.StringValue(cred.Label)
	state.Issuer = types.StringValue(cred.Issuer)
	state.Audience = types.StringValue(cred.Audience)
	claimConditions, diags := types.MapValueFrom(ctx, types.StringType, cred.ClaimConditions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ClaimConditions = claimConditions
	state.Role = types.StringValue(cred.Role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *FederatedCredentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Unreachable: every attribute carries a RequiresReplace plan modifier,
	// since the platform has no update endpoint for federated credentials.
	resp.Diagnostics.AddError(
		"Federated credentials cannot be updated in place",
		"This is a bug in the provider: all attribute changes should have planned a replacement.",
	)
}

func (r *FederatedCredentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveFederatedCredential(ctx,
		state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting federated credential", err.Error())
	}
}

func (r *FederatedCredentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Federated credentials are addressed by organization and credential id:
	// `terraform import tenzir_federated_credential.example <organization-id>/<credential-id>`
	organizationID, credentialID, ok := strings.Cut(req.ID, "/")
	if !ok || organizationID == "" || credentialID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <organization-id>/<credential-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), organizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), credentialID)...)
}
