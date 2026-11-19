package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure  = &deploymentKeyResource{}
	_ resource.ResourceWithModifyPlan = &deploymentKeyResource{}
)

func NewDeploymentKeyResource() resource.Resource {
	return &deploymentKeyResource{}
}

type deploymentKeyResource struct {
	client *client.Client
}

type deploymentKeyModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	WorkspaceID    types.String `tfsdk:"workspace_id"`
	DeploymentID   types.String `tfsdk:"deployment_id"`
	Triggers       types.Map    `tfsdk:"triggers"`
	Key            types.String `tfsdk:"key"`
}

func (r *deploymentKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment_key"
}

func (r *deploymentKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The key a self-hosted deployment connects with, which it presents as " +
			"`x-tenzir-deployment-key`. The deployment reads it from the file that its `--key-file` option names, " +
			"together with `--id` for the deployment ID. " +
			"Creating this resource mints a new key, which stops the previous one working: a deployment " +
			"connected with it stays connected, but cannot reconnect until it has the new one. Changing " +
			"`triggers` mints another.\n\n" +
			"The platform shows a key only once, so the state holds it and it cannot be imported. " +
			"Destroying the resource only removes the key from the state: the deployment keeps " +
			"connecting with it until a new key is minted or the deployment is deleted. A hosted " +
			"deployment's key belongs to the platform, which refuses to rotate it.\n\n" +
			"Needs `Deployment.rotateKey` and `Deployment.read`; parents given by slug also need `Workspace.read`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The deployment ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the deployment's workspace.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"deployment_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the self-hosted deployment.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"triggers": schema.MapAttribute{
				MarkdownDescription: "Arbitrary values whose change mints a new key, for rotating it on a schedule " +
					"or after a leak.",
				Optional:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The key.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *deploymentKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan keeps the parents as the state names them while they name the
// same organization and workspace, so that naming them otherwise mints no key.
func (r *deploymentKeyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, true)
}

func (m *deploymentKeyModel) parents() parentRefs {
	return parentRefs{Organization: m.OrganizationID, Workspace: m.WorkspaceID}
}

func (r *deploymentKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan deploymentKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, plan.parents(), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	key, err := r.client.RotateDeploymentKey(ctx, deploymentRef(ids, plan.DeploymentID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Cannot mint the deployment key", err.Error())
		return
	}
	plan.ID = plan.DeploymentID
	plan.Key = types.StringValue(key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read drops the key once its deployment is gone; the key itself cannot be
// read back.
func (r *deploymentKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state deploymentKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, state.parents())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.ReadDeploymentDetails(ctx, deploymentRef(ids, state.DeploymentID.ValueString()))
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the deployment", err.Error())
	}
}

// Update is never called, because every attribute replaces the resource.
func (r *deploymentKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.State.Raw = req.Plan.Raw
}

func (r *deploymentKeyResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
