package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &SecretStoreResource{}
var _ resource.ResourceWithValidateConfig = &SecretStoreResource{}
var _ resource.ResourceWithImportState = &SecretStoreResource{}

func NewSecretStoreResource() resource.Resource {
	return &SecretStoreResource{}
}

// SecretStoreResource manages an external secret store (AWS Secrets Manager
// or HashiCorp Vault) in a Tenzir Platform workspace.
type SecretStoreResource struct {
	client *client.Client
}

type SecretStoreResourceModel struct {
	ID          types.String           `tfsdk:"id"`
	WorkspaceID types.String           `tfsdk:"workspace_id"`
	Name        types.String           `tfsdk:"name"`
	IsWritable  types.Bool             `tfsdk:"is_writable"`
	Default     types.Bool             `tfsdk:"default"`
	AWS         *SecretStoreAWSModel   `tfsdk:"aws"`
	Vault       *SecretStoreVaultModel `tfsdk:"vault"`
}

type SecretStoreAWSModel struct {
	Region         types.String `tfsdk:"region"`
	AssumedRoleARN types.String `tfsdk:"assumed_role_arn"`
}

type SecretStoreVaultModel struct {
	Address    types.String `tfsdk:"address"`
	Namespace  types.String `tfsdk:"namespace"`
	Mount      types.String `tfsdk:"mount"`
	AuthMethod types.String `tfsdk:"auth_method"`
	Token      types.String `tfsdk:"token"`
	RoleID     types.String `tfsdk:"role_id"`
	SecretID   types.String `tfsdk:"secret_id"`
}

func (r *SecretStoreResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret_store"
}

func (r *SecretStoreResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An external secret store in a Tenzir Platform workspace, backed by AWS " +
			"Secrets Manager (`aws`) or HashiCorp Vault (`vault`); exactly one of the two " +
			"blocks must be set. Every workspace additionally has a platform-managed " +
			"built-in store that is not represented by this resource.\n\n" +
			"The platform has no update operation for secret stores, so changing any " +
			"attribute except `default` replaces the store. The workspace's current " +
			"default store cannot be deleted: to destroy a store with `default = true`, " +
			"first move the default to another store (e.g. the built-in store, outside of " +
			"Terraform).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the secret store.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Description: "Identifier of the workspace the secret store belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the secret store. Defaults to a name derived " +
					"from the store type.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"is_writable": schema.BoolAttribute{
				Description: "Whether secrets can be added to and updated in the store " +
					"through the platform. Defaults to `false` (read-only). Only supported " +
					"for `aws` stores; the platform always treats `vault` stores as " +
					"read-only.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"default": schema.BoolAttribute{
				Description: "Whether this store is the workspace's default secret store. " +
					"Setting it to `true` moves the default to this store (an in-place " +
					"update). The platform has no operation to unset a default store, only " +
					"to move it: changing `true` to `false` is an error unless another " +
					"store was already marked as the default.",
				Optional: true,
				Computed: true,
			},
			"aws": schema.SingleNestedAttribute{
				Description: "Configuration of an AWS Secrets Manager store.",
				Optional:    true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"region": schema.StringAttribute{
						Description: "AWS region of the target Secrets Manager.",
						Required:    true,
					},
					"assumed_role_arn": schema.StringAttribute{
						Description: "ARN of the IAM role the platform assumes to access " +
							"the secrets.",
						Required: true,
					},
				},
			},
			"vault": schema.SingleNestedAttribute{
				Description: "Configuration of a HashiCorp Vault store. The credential " +
					"values (`token` and `secret_id`) are write-only at the API level: the " +
					"platform never returns them, so changes made outside of Terraform are " +
					"not detected, and they stay unset after an import (planning a " +
					"replacement if the configuration sets them).",
				Optional: true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"address": schema.StringAttribute{
						Description: "URL of the Vault server, e.g. `https://vault.example.com:8200`.",
						Required:    true,
					},
					"namespace": schema.StringAttribute{
						Description: "Vault namespace (Vault Enterprise only).",
						Optional:    true,
					},
					"mount": schema.StringAttribute{
						Description: "KV v2 mount point in Vault, e.g. `secret`.",
						Required:    true,
					},
					"auth_method": schema.StringAttribute{
						Description: "Authentication method, `token` or `approle`.",
						Required:    true,
					},
					"token": schema.StringAttribute{
						Description: "Vault token; required for the `token` auth method. " +
							"Stored in the Terraform state; treat the state as sensitive.",
						Optional:  true,
						Sensitive: true,
					},
					"role_id": schema.StringAttribute{
						Description: "AppRole role id; required for the `approle` auth method.",
						Optional:    true,
					},
					"secret_id": schema.StringAttribute{
						Description: "AppRole secret id; required for the `approle` auth " +
							"method. Stored in the Terraform state; treat the state as " +
							"sensitive.",
						Optional:  true,
						Sensitive: true,
					},
				},
			},
		},
	}
}

func (r *SecretStoreResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var aws, vault types.Object
	var isWritable types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("aws"), &aws)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("vault"), &vault)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("is_writable"), &isWritable)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unknown values (e.g. whole blocks assigned from unresolved module
	// outputs) cannot be validated until apply; skip the affected checks.
	if !aws.IsUnknown() && !vault.IsUnknown() && aws.IsNull() == vault.IsNull() {
		resp.Diagnostics.AddError(
			"Invalid secret store configuration",
			"Exactly one of the `aws` and `vault` blocks must be set.",
		)
		return
	}
	if !vault.IsNull() && !vault.IsUnknown() {
		if !isWritable.IsNull() && !isWritable.IsUnknown() && isWritable.ValueBool() {
			resp.Diagnostics.AddAttributeError(
				path.Root("is_writable"),
				"Invalid secret store configuration",
				"The platform always treats `vault` stores as read-only; `is_writable = true` is only supported for `aws` stores.",
			)
		}
		r.validateVaultAuth(vault, resp)
	}
}

// validateVaultAuth checks that the credential attributes of a known, non-null
// `vault` block match its auth_method.
func (r *SecretStoreResource) validateVaultAuth(vault types.Object, resp *resource.ValidateConfigResponse) {
	attrs := vault.Attributes()
	authMethod, ok := attrs["auth_method"].(types.String)
	if !ok || authMethod.IsNull() || authMethod.IsUnknown() {
		return
	}
	set := func(name string) bool {
		v, ok := attrs[name].(types.String)
		// Unknown counts as set: it resolves to a value at apply time.
		return ok && !v.IsNull()
	}
	switch authMethod.ValueString() {
	case "token":
		if !set("token") {
			resp.Diagnostics.AddAttributeError(
				path.Root("vault").AtName("token"),
				"Invalid secret store configuration",
				"The `token` auth method requires `token` to be set.",
			)
		}
		for _, name := range []string{"role_id", "secret_id"} {
			if set(name) {
				resp.Diagnostics.AddAttributeError(
					path.Root("vault").AtName(name),
					"Invalid secret store configuration",
					fmt.Sprintf("`%s` must not be set with the `token` auth method.", name),
				)
			}
		}
	case "approle":
		for _, name := range []string{"role_id", "secret_id"} {
			if !set(name) {
				resp.Diagnostics.AddAttributeError(
					path.Root("vault").AtName(name),
					"Invalid secret store configuration",
					fmt.Sprintf("The `approle` auth method requires `%s` to be set.", name),
				)
			}
		}
		if set("token") {
			resp.Diagnostics.AddAttributeError(
				path.Root("vault").AtName("token"),
				"Invalid secret store configuration",
				"`token` must not be set with the `approle` auth method.",
			)
		}
	default:
		resp.Diagnostics.AddAttributeError(
			path.Root("vault").AtName("auth_method"),
			"Invalid secret store configuration",
			fmt.Sprintf("Unsupported auth method %q; must be `token` or `approle`.", authMethod.ValueString()),
		)
	}
}

func (r *SecretStoreResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *SecretStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecretStoreResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var storeType string
	options := make(map[string]string)
	switch {
	case plan.AWS != nil:
		storeType = "aws"
		options["region"] = plan.AWS.Region.ValueString()
		options["assumed_role_arn"] = plan.AWS.AssumedRoleARN.ValueString()
	case plan.Vault != nil:
		storeType = "vault"
		options["address"] = plan.Vault.Address.ValueString()
		options["mount"] = plan.Vault.Mount.ValueString()
		options["auth_method"] = plan.Vault.AuthMethod.ValueString()
		if !plan.Vault.Namespace.IsNull() {
			options["namespace"] = plan.Vault.Namespace.ValueString()
		}
		if !plan.Vault.Token.IsNull() {
			options["token"] = plan.Vault.Token.ValueString()
		}
		if !plan.Vault.RoleID.IsNull() {
			options["role_id"] = plan.Vault.RoleID.ValueString()
		}
		if !plan.Vault.SecretID.IsNull() {
			options["secret_id"] = plan.Vault.SecretID.ValueString()
		}
	default:
		// Unreachable after ValidateConfig, unless both blocks were unknown
		// during validation and resolved to null.
		resp.Diagnostics.AddError(
			"Invalid secret store configuration",
			"Exactly one of the `aws` and `vault` blocks must be set.",
		)
		return
	}

	var name *string
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		name = plan.Name.ValueStringPointer()
	}
	makeDefault := !plan.Default.IsNull() && !plan.Default.IsUnknown() && plan.Default.ValueBool()

	workspaceID := plan.WorkspaceID.ValueString()
	id, err := r.client.AddSecretStore(ctx, workspaceID, storeType, name,
		plan.IsWritable.ValueBool(), makeDefault, options)
	if err != nil {
		resp.Diagnostics.AddError("Error creating secret store", err.Error())
		return
	}
	plan.ID = types.StringValue(id)

	// Fill in the server-computed attributes (the defaulted name and the
	// default-store marker) from the store list.
	store, isDefault, err := r.client.GetSecretStore(ctx, workspaceID, id)
	if err == nil && store == nil {
		err = fmt.Errorf("store %s missing from the workspace's store list", id)
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading back created secret store",
			"The secret store was created with id "+id+" but reading it back failed, "+
				"so it is not tracked in state. Import it with `terraform import` "+
				"or delete it manually. Error: "+err.Error(),
		)
		return
	}
	plan.Name = types.StringValue(store.Name)
	plan.IsWritable = types.BoolValue(store.IsWritable)
	plan.Default = types.BoolValue(isDefault)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecretStoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	store, isDefault, err := r.client.GetSecretStore(ctx,
		state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading secret store", err.Error())
		return
	}
	if store == nil {
		// The store (or its whole workspace) was deleted outside of
		// Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(store.Name)
	state.IsWritable = types.BoolValue(store.IsWritable)
	state.Default = types.BoolValue(isDefault)
	switch store.Type {
	case "aws":
		state.AWS = &SecretStoreAWSModel{
			Region:         types.StringValue(store.Options.Region),
			AssumedRoleARN: types.StringValue(store.Options.AssumedRoleARN),
		}
		state.Vault = nil
	case "vault":
		vault := SecretStoreVaultModel{
			Address:    types.StringValue(store.Options.Address),
			Namespace:  types.StringPointerValue(store.Options.Namespace),
			Mount:      types.StringValue(store.Options.Mount),
			AuthMethod: types.StringValue(store.Options.AuthMethod),
			Token:      types.StringNull(),
			RoleID:     types.StringNull(),
			SecretID:   types.StringNull(),
		}
		// The platform masks credential values in its responses, so keep
		// `token` and `secret_id` from the prior state: changes made outside
		// of Terraform are not detected, and imported stores leave them
		// unset.
		if store.Options.TokenAuth != nil && state.Vault != nil {
			vault.Token = state.Vault.Token
		}
		if store.Options.AppRoleAuth != nil {
			vault.RoleID = types.StringValue(store.Options.AppRoleAuth.RoleID)
			if state.Vault != nil {
				vault.SecretID = state.Vault.SecretID
			}
		}
		state.Vault = &vault
		state.AWS = nil
	default:
		// Only reachable by importing the id of the platform-managed
		// built-in ("tenzir") store, which is not an external store.
		resp.Diagnostics.AddError(
			"Not an external secret store",
			fmt.Sprintf("Secret store %s has type %q; only external stores (aws, vault) "+
				"can be managed by this resource.", store.ID, store.Type),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecretStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SecretStoreResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only `default` is updatable in place; all other attributes carry a
	// RequiresReplace plan modifier.
	switch {
	case plan.Default.ValueBool() && !state.Default.ValueBool():
		err := r.client.SelectSecretStore(ctx,
			plan.WorkspaceID.ValueString(), plan.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error selecting default secret store", err.Error())
			return
		}
	case !plan.Default.ValueBool() && state.Default.ValueBool():
		resp.Diagnostics.AddError(
			"Cannot unset the default secret store",
			"The platform has no operation to unset a workspace's default secret store, "+
				"only to move it. Mark another store as the default (`default = true`) "+
				"instead of setting this one to `false`.",
		)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecretStoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSecretStore(ctx,
		state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		detail := err.Error()
		if state.Default.ValueBool() {
			detail += "\n\nThis store is the workspace's default secret store, which " +
				"cannot be deleted. Move the default to another store first (e.g. the " +
				"built-in store, outside of Terraform)."
		}
		resp.Diagnostics.AddError("Error deleting secret store", detail)
	}
}

func (r *SecretStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Secret stores are addressed by workspace and store id:
	// `terraform import tenzir_secret_store.example <workspace-id>/<store-id>`
	// Vault credential values (`token`, `secret_id`) cannot be read back and
	// stay unset in the imported state.
	workspaceID, storeID, ok := strings.Cut(req.ID, "/")
	if !ok || workspaceID == "" || storeID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <workspace-id>/<store-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), storeID)...)
}
