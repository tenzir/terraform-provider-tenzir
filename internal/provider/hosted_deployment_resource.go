package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &hostedDeploymentResource{}
	_ resource.ResourceWithImportState = &hostedDeploymentResource{}
	_ resource.ResourceWithModifyPlan  = &hostedDeploymentResource{}
)

const defaultHostedCreateTimeout = 15 * time.Minute

var reachablePollInterval = 5 * time.Second

func NewHostedDeploymentResource() resource.Resource {
	return &hostedDeploymentResource{}
}

type hostedDeploymentResource struct {
	client *client.Client
}

type hostedDeploymentModel struct {
	deploymentModel
	Size     types.String   `tfsdk:"size"`
	Region   types.String   `tfsdk:"region"`
	Image    types.String   `tfsdk:"image"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *hostedDeploymentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosted_deployment"
}

func (r *hostedDeploymentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := deploymentAttributes()
	attributes["size"] = schema.StringAttribute{
		MarkdownDescription: "The size it runs as, one of those `tenzir_deployment_types` offers. Defaults to the " +
			"offer's `default_size`. Changing it restarts the deployment; its pipelines resume once it is back.",
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
	attributes["region"] = schema.StringAttribute{
		MarkdownDescription: "The cloud region it runs in, one of those `tenzir_deployment_types` offers. Defaults " +
			"to the first. Changing it replaces the deployment.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
			stringplanmodifier.RequiresReplace(),
		},
	}
	attributes["image"] = schema.StringAttribute{
		MarkdownDescription: "A `tenzir/tenzir` image to run instead of the one the platform hosts deployments " +
			"with, such as a `reference` from `tenzir_deployment_images`. The platform does not report the image " +
			"back, so Terraform keeps what it was given: changing it replaces the deployment, and after an " +
			"import Terraform takes the configured image as given.",
		Optional: true,
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
				resp.RequiresReplace = !req.StateValue.IsNull()
			}, "Changing the image replaces the deployment.", "Changing the image replaces the deployment."),
		},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A deployment that Tenzir runs. The platform starts it and hands it its key, so it " +
			"needs no install. Creating it waits until the deployment connects, for at most the `create` " +
			"timeout (15 minutes by default). Destroying it stops and removes it.\n\n" +
			"Needs `Workspace.createHostedDeployment` and `Deployment.read`, `Deployment.readTracker`, " +
			"`Deployment.update`, `Deployment.changeTracker`, `Deployment.changeType` and `Deployment.delete`; " +
			"parents given by slug also need `Workspace.read`. " +
			"The platform hosts deployments only on Enterprise; `tenzir_deployment_types` says whether it does.",
		Attributes: attributes,
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *hostedDeploymentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *hostedDeploymentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, true)
}

func (m *hostedDeploymentModel) set(d *client.DeploymentDetails) {
	m.deploymentModel.set(d)
	m.Size = optionalString(d.Size)
	m.Region = optionalString(d.Region)
}

func (r *hostedDeploymentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostedDeploymentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, defaultHostedCreateTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, plan.parents(), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	d, err := r.client.CreateHostedDeployment(ctx, ids.Organization, ids.Workspace, client.NewHostedDeployment{
		NewDeployment: plan.newDeployment(),
		Image:         stringPointer(plan.Image),
		Size:          stringPointer(plan.Size),
		Region:        stringPointer(plan.Region),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the hosted deployment", err.Error())
		return
	}
	details, err := r.client.ReadDeploymentDetails(ctx, d.ID)
	if err != nil {
		plan.ID = types.StringValue(d.ID)
		plan.CreatedAt = types.StringNull()
		if plan.Size.IsUnknown() {
			plan.Size = types.StringNull()
		}
		if plan.Region.IsUnknown() {
			plan.Region = types.StringNull()
		}
		resp.Diagnostics.AddError("Cannot read the new hosted deployment", err.Error())
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	plan.set(details)
	// The deployment exists from here on, so a failed wait taints it rather
	// than losing it.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := waitUntilReachable(ctx, r.client, d.ID, timeout); err != nil {
		resp.Diagnostics.AddError("The hosted deployment did not connect", err.Error())
	}
}

// waitUntilReachable polls a deployment until it is connected to the
// platform.
func waitUntilReachable(ctx context.Context, c *client.Client, id string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(reachablePollInterval)
	defer ticker.Stop()
	for {
		details, err := c.ReadDeploymentDetails(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("deployment %s did not connect within %s", id, timeout)
			}
			return err
		}
		if details.Reachable {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("deployment %s did not connect within %s", id, timeout)
		case <-ticker.C:
		}
	}
}

func (r *hostedDeploymentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostedDeploymentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	details, ok := readDeployment(ctx, r.client, state.ID.ValueString(), resp)
	if !ok {
		return
	}
	state.set(details)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *hostedDeploymentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state hostedDeploymentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := plan.deploymentModel.update(ctx, r.client, &state.deploymentModel)
	if err == nil && !plan.Size.IsUnknown() && !plan.Size.Equal(state.Size) {
		err = r.client.ChangeDeploymentType(ctx, state.ID.ValueString(), plan.Size.ValueString())
		if err != nil {
			err = fmt.Errorf("changing the size: %w", err)
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the hosted deployment", err.Error())
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	if plan.Size.IsUnknown() {
		plan.Size = state.Size
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *hostedDeploymentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostedDeploymentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteDeployment(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the hosted deployment", err.Error())
	}
}

// ImportState takes "<organization>/<workspace>/<deployment>", the first two
// each an ID or a slug, and stores IDs. The image stays null, because the
// platform does not report it.
func (r *hostedDeploymentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(importDeployment(ctx, r.client, req.ID, hostingTenzir, resp)...)
}
