package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure      = &secretResource{}
	_ resource.ResourceWithImportState    = &secretResource{}
	_ resource.ResourceWithModifyPlan     = &secretResource{}
	_ resource.ResourceWithValidateConfig = &secretResource{}
)

// privateValueDrift marks a secret whose version moved outside Terraform, so
// the next apply writes the configured value again.
const privateValueDrift = "value_drift"

func NewSecretResource() resource.Resource {
	return &secretResource{}
}

type secretResource struct {
	client *client.Client
}

type secretModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	WorkspaceID    types.String `tfsdk:"workspace_id"`
	DeploymentID   types.String `tfsdk:"deployment_id"`
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
	ValueVersion   types.Int64  `tfsdk:"value_version"`
	ReadableBy     types.Object `tfsdk:"readable_by"`
	Version        types.Int64  `tfsdk:"version"`
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A secret that pipelines read with `secret(\"<name>\")`. An organization, a " +
			"workspace or a deployment holds it: the most specific of `organization_id`, `workspace_id` " +
			"and `deployment_id` that is set. A deployment reads the closest secret of a name.\n\n" +
			"The value is write-only, so it never reaches the state; this needs Terraform 1.11 or later. " +
			"The platform counts every write in `version`. When it changes outside Terraform, the next " +
			"apply writes the configured value again. To change the value, change `value` and bump " +
			"`value_version`.\n\n" +
			"Needs `Organization.createSecret`, `Workspace.createSecret` or `Deployment.createSecret` for the " +
			"owner, and the `OrgSecret`, `WorkspaceSecret` or `DeploymentSecret` actions `read`, `update` " +
			"and `delete` on the secret.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The secret ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the workspace that holds the secret, or that the deployment belongs to.",
				Optional:            true,
				PlanModifiers:       replace,
			},
			"deployment_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the deployment that holds the secret. Needs `workspace_id`.",
				Optional:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.AlsoRequires(path.MatchRoot("workspace_id"))},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name pipelines read the secret by.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The value. Write-only: Terraform sends it but never stores it.",
				Required:            true,
				Sensitive:           true,
				WriteOnly:           true,
			},
			"value_version": schema.Int64Attribute{
				MarkdownDescription: "Change this to write a changed `value`, which Terraform cannot see by itself.",
				Optional:            true,
			},
			"readable_by": readableBySchema("secret"),
			"version": schema.Int64Attribute{
				MarkdownDescription: "How often the platform has written the secret.",
				Computed:            true,
			},
		},
	}
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *secretResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config secretModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.ReadableBy.IsNull() || config.ReadableBy.IsUnknown() {
		return
	}
	if !config.DeploymentID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("readable_by"), "Readers of a deployment secret",
			"Only its deployment reads a deployment secret, so it takes no `readable_by`.")
		return
	}
	var readers readableByModel
	resp.Diagnostics.Append(config.ReadableBy.As(ctx, &readers, defaultObjectAsOptions)...)
	if !config.WorkspaceID.IsNull() && !readers.Workspaces.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("readable_by").AtName("workspaces"), "Workspaces reading a workspace secret",
			"A workspace secret reaches only the workspace's own deployments; list them in `deployments`.")
	}
}

func (m *secretModel) scope() client.SecretScope {
	return client.SecretScope{
		Organization: m.OrganizationID.ValueString(),
		Workspace:    m.WorkspaceID.ValueString(),
		Deployment:   m.DeploymentID.ValueString(),
	}
}

func (m *secretModel) write(ctx context.Context, withValue bool) (client.SecretWrite, error) {
	var w client.SecretWrite
	if withValue {
		w.Value = stringPointer(m.Value)
	}
	if m.DeploymentID.IsNull() {
		readers, diags := readableByToAPI(ctx, m.ReadableBy)
		if diags.HasError() {
			return w, diagsError(diags)
		}
		w.ReadableBy = &readers
	}
	return w, nil
}

func (m *secretModel) set(ctx context.Context, s *client.Secret) error {
	m.ID = types.StringValue(s.ID)
	m.Name = types.StringValue(s.Name)
	m.Version = types.Int64Value(s.Version)
	if m.DeploymentID.IsNull() {
		readers, diags := readableByFromAPI(ctx, s.ReadableBy, m.ReadableBy)
		if diags.HasError() {
			return diagsError(diags)
		}
		m.ReadableBy = readers
	}
	return nil
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Value = config.Value
	w, err := plan.write(ctx, true)
	if err != nil {
		resp.Diagnostics.AddError("Invalid readers", err.Error())
		return
	}
	s, err := r.client.CreateSecret(ctx, plan.scope(), plan.Name.ValueString(), w)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the secret", err.Error())
		return
	}
	if err := plan.set(ctx, s); err != nil {
		resp.Diagnostics.AddError("Cannot read the secret's readers", err.Error())
		return
	}
	plan.Value = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.ReadSecret(ctx, state.scope(), state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the secret", err.Error())
		return
	}
	if !state.Version.IsNull() && state.Version.ValueInt64() != s.Version {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateValueDrift, []byte("true"))...)
	}
	if err := state.set(ctx, s); err != nil {
		resp.Diagnostics.AddError("Cannot read the secret's readers", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ModifyPlan plans a write of the value when its version moved outside
// Terraform.
func (r *secretResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	drift, diags := req.Private.GetKey(ctx, privateValueDrift)
	resp.Diagnostics.Append(diags...)
	if len(drift) == 0 {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("version"), types.Int64Unknown())...)
}

func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	drift, diags := req.Private.GetKey(ctx, privateValueDrift)
	resp.Diagnostics.Append(diags...)
	plan.Value = config.Value
	w, err := plan.write(ctx, len(drift) > 0 || !plan.ValueVersion.Equal(state.ValueVersion))
	if err != nil {
		resp.Diagnostics.AddError("Invalid readers", err.Error())
		return
	}
	s, err := r.client.SetSecret(ctx, plan.scope(), plan.Name.ValueString(), w)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the secret", err.Error())
		return
	}
	if err := plan.set(ctx, s); err != nil {
		resp.Diagnostics.AddError("Cannot read the secret's readers", err.Error())
		return
	}
	plan.Value = types.StringNull()
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateValueDrift, nil)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteSecret(ctx, state.scope(), state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the secret", err.Error())
	}
}

// ImportState takes "<organization>/<name>", "<organization>/<workspace>/<name>"
// or "<organization>/<workspace>/<deployment>/<name>", and stores IDs.
func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) < 2 || len(parts) > 4 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected <organization>/<name>, <organization>/<workspace>/<name> or "+
				"<organization>/<workspace>/<deployment>/<name>, got "+req.ID)
		return
	}
	scope := client.SecretScope{Organization: parts[0]}
	if len(parts) >= 3 {
		scope.Workspace = parts[1]
	}
	if len(parts) == 4 {
		scope.Deployment = parts[2]
	}
	name := parts[len(parts)-1]
	s, err := r.client.ReadSecret(ctx, scope, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the secret", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), s.Owner.OrgID)...)
	if s.Owner.WorkspaceID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), s.Owner.WorkspaceID)...)
	}
	if s.Owner.DeploymentID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("deployment_id"), s.Owner.DeploymentID)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), s.Name)...)
}
