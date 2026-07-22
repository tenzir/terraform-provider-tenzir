package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.Resource                   = &SecretResource{}
	_ resource.ResourceWithValidateConfig = &SecretResource{}
)

// SecretResource deliberately does not implement
// resource.ResourceWithImportState: the platform never returns secret
// values, so an imported secret could not populate the `value` or
// `value_wo` attributes and importing is not sensible.

func NewSecretResource() resource.Resource {
	return &SecretResource{}
}

// SecretResource manages a secret in a Tenzir Platform workspace secret
// store.
type SecretResource struct {
	client *client.Client
}

type SecretResourceModel struct {
	ID             types.String `tfsdk:"id"`
	WorkspaceID    types.String `tfsdk:"workspace_id"`
	StoreID        types.String `tfsdk:"store_id"`
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
	ValueWo        types.String `tfsdk:"value_wo"`
	ValueWoVersion types.String `tfsdk:"value_wo_version"`
}

func (r *SecretResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *SecretResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A secret in a Tenzir Platform workspace secret store, usable from " +
			"pipelines via the `secret` function. The secret is placed in the store given " +
			"by `store_id`, or in the workspace's default store when unset; the store must " +
			"be writable.\n\n" +
			"The secret value is given either as `value`, which is stored in the Terraform " +
			"state, or as the write-only `value_wo` (together with `value_wo_version`), " +
			"which never persists to the plan or state. Prefer `value_wo` to keep the " +
			"plaintext out of the state; write-only attributes require Terraform >= 1.11.\n\n" +
			"This resource cannot be imported: the platform never returns secret values, " +
			"so an imported secret could not populate the `value` or `value_wo` attributes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the secret.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Description: "Identifier of the workspace the secret belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"store_id": schema.StringAttribute{
				Description: "Identifier of the secret store holding the secret. Unset means " +
					"the workspace's default secret store. Note that with `store_id` unset, " +
					"the secret is always looked up in the workspace's *current* default " +
					"store, so moving the workspace default to another store makes the " +
					"secret appear deleted and plans a re-creation in the new default store.",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the secret, as referenced from pipelines. The platform " +
					"has no rename operation for secrets, so changing the name replaces " +
					"(deletes and re-creates) the secret.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Description: "Value of the secret. Exactly one of `value` and `value_wo` " +
					"must be set. The value is write-only at the API level: the platform " +
					"never returns secret values, so changes made outside of Terraform are " +
					"not detected (there is no drift detection on the value). Stored in the " +
					"Terraform state; treat the state as sensitive, or use `value_wo` " +
					"instead to keep the plaintext out of the state.",
				Optional:  true,
				Sensitive: true,
			},
			"value_wo": schema.StringAttribute{
				Description: "Write-only value of the secret, never persisted to the plan " +
					"or state. Requires Terraform >= 1.11, the first release with write-only " +
					"attribute support. Exactly one of `value` and `value_wo` must be set. " +
					"Because the value is not stored, Terraform cannot detect changes to it: " +
					"to update the secret, change `value_wo_version` (which must be set " +
					"together with `value_wo`) whenever `value_wo` changes.",
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
			},
			"value_wo_version": schema.StringAttribute{
				Description: "Version marker for `value_wo`, required when `value_wo` is " +
					"set. Changing `value_wo` alone produces no diff since write-only " +
					"values are never stored; change `value_wo_version` (e.g. bump a " +
					"counter or timestamp) to trigger an in-place update that sends the " +
					"current `value_wo` to the platform. The version is an opaque marker " +
					"and is not sent to the platform.",
				Optional: true,
			},
		},
	}
}

func (r *SecretResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

// ValidateConfig enforces the `value` / `value_wo` contract. This is
// hand-rolled instead of using resource-level config validators because
// `value_wo` is write-only, so its value must be read from the config.
func (r *SecretResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var value, valueWo, valueWoVersion types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value"), &value)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value_wo"), &valueWo)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value_wo_version"), &valueWoVersion)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unknown values (e.g. attributes assigned from unresolved module
	// outputs) cannot be validated until apply; skip the affected checks.
	if !value.IsUnknown() && !valueWo.IsUnknown() && value.IsNull() == valueWo.IsNull() {
		resp.Diagnostics.AddError(
			"Invalid secret configuration",
			"Exactly one of `value` and `value_wo` must be set.",
		)
	}
	if !valueWo.IsUnknown() && !valueWoVersion.IsUnknown() {
		if !valueWo.IsNull() && valueWoVersion.IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root("value_wo_version"),
				"Invalid secret configuration",
				"`value_wo_version` must be set when `value_wo` is set: since the "+
					"write-only `value_wo` is never stored, changing `value_wo_version` "+
					"is what triggers an update of the secret value.",
			)
		}
		if valueWo.IsNull() && !valueWoVersion.IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root("value_wo_version"),
				"Invalid secret configuration",
				"`value_wo_version` must only be set together with `value_wo`.",
			)
		}
	}
}

// secretValue returns the secret value to send to the platform: `value`
// from the plan when set, otherwise the write-only `value_wo` read from the
// given config (write-only values never appear in the plan or state, only
// in the config). ValidateConfig guarantees that exactly one of the two is
// set.
func secretValue(ctx context.Context, plan *SecretResourceModel, config tfsdk.Config) (string, diag.Diagnostics) {
	if !plan.Value.IsNull() {
		return plan.Value.ValueString(), nil
	}
	var valueWo types.String
	diags := config.GetAttribute(ctx, path.Root("value_wo"), &valueWo)
	return valueWo.ValueString(), diags
}

func (r *SecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	value, diags := secretValue(ctx, &plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.AddSecret(ctx,
		plan.WorkspaceID.ValueString(),
		plan.StoreID.ValueStringPointer(),
		plan.Name.ValueString(),
		value)
	if err != nil {
		resp.Diagnostics.AddError("Error creating secret", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secret, err := r.client.GetSecret(ctx,
		state.WorkspaceID.ValueString(),
		state.StoreID.ValueStringPointer(),
		state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading secret", err.Error())
		return
	}
	if secret == nil {
		// The secret (or its store, or the whole workspace) was deleted
		// outside of Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(secret.Name)
	// The platform never returns secret values, so keep `value` from the
	// prior state: changes made outside of Terraform are not detected.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	value, diags := secretValue(ctx, &plan, req.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only the value (via `value`, or `value_wo` triggered by a
	// `value_wo_version` change) is updatable in place; all other attributes
	// carry a RequiresReplace plan modifier.
	err := r.client.UpdateSecret(ctx,
		plan.WorkspaceID.ValueString(),
		plan.StoreID.ValueStringPointer(),
		plan.ID.ValueString(),
		value)
	if err != nil {
		resp.Diagnostics.AddError("Error updating secret", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveSecret(ctx,
		state.WorkspaceID.ValueString(),
		state.StoreID.ValueStringPointer(),
		state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting secret", err.Error())
	}
}
