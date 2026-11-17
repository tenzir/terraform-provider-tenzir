package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &deploymentResource{}
	_ resource.ResourceWithImportState = &deploymentResource{}
	_ resource.ResourceWithModifyPlan  = &deploymentResource{}
)

func NewDeploymentResource() resource.Resource {
	return &deploymentResource{}
}

type deploymentResource struct {
	client *client.Client
}

// deploymentModel holds what self-hosted and hosted deployments share.
type deploymentModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	WorkspaceID    types.String `tfsdk:"workspace_id"`
	Name           types.String `tfsdk:"name"`
	Branch         types.String `tfsdk:"branch"`
	AutoDeploy     types.Bool   `tfsdk:"auto_deploy"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

const (
	hostingCustomer = "customer"
	hostingTenzir   = "tenzir"
)

func (r *deploymentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment"
}

// deploymentAttributes are the attributes that both deployment resources
// have.
func deploymentAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "The deployment ID.",
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"organization_id": schema.StringAttribute{
			MarkdownDescription: "The ID or slug of the organization.",
			Required:            true,
		},
		"workspace_id": schema.StringAttribute{
			MarkdownDescription: "The ID or slug of the workspace the deployment belongs to.",
			Required:            true,
		},
		"name": schema.StringAttribute{
			MarkdownDescription: "The display name. Renaming keeps the deployment's ID and key.",
			Required:            true,
		},
		"branch": schema.StringAttribute{
			MarkdownDescription: "The branch of the workspace repository whose configuration the deployment runs. " +
				"Defaults to `main`. The platform refuses to switch to a branch without commits.",
			Optional: true,
			Computed: true,
			Default:  stringdefault.StaticString("main"),
		},
		"auto_deploy": schema.BoolAttribute{
			MarkdownDescription: "Whether new commits on `branch` deploy on their own. Defaults to `true`. " +
				"When false, the deployment keeps the configuration it has until someone deploys a commit.",
			Optional: true,
			Computed: true,
			Default:  booldefault.StaticBool(true),
		},
		"created_at": schema.StringAttribute{
			MarkdownDescription: "When the deployment was created.",
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
	}
}

func (r *deploymentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A self-hosted deployment: a Tenzir node that runs where you install it and " +
			"connects to the platform. It cannot connect until it has a key, which `tenzir_deployment_key` " +
			"mints, or which the install command in the web UI fetches. Destroying it hands a connected " +
			"deployment an empty configuration, so it stops its pipelines. For a deployment that Tenzir runs, " +
			"use `tenzir_hosted_deployment`; for its connection status, the `tenzir_deployment` data source.\n\n" +
			"Needs `Workspace.createDeployment` and `Deployment.read`, `Deployment.readTracker`, " +
			"`Deployment.update`, `Deployment.changeTracker` and `Deployment.delete`; parents given by slug " +
			"also need `Workspace.read`.",
		Attributes: deploymentAttributes(),
	}
}

func (r *deploymentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *deploymentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, true)
}

func (m *deploymentModel) parents() parentRefs {
	return parentRefs{Organization: m.OrganizationID, Workspace: m.WorkspaceID}
}

// ref names the deployment by the IDs of the parents it recorded.
func (m *deploymentModel) ref(ctx context.Context, c *client.Client, private privateGetter) (client.DeploymentRef, diag.Diagnostics) {
	ids, diags := knownParents(ctx, c, private, m.parents())
	return deploymentRef(ids, m.ID.ValueString()), diags
}

func deploymentRef(ids parentIDs, id string) client.DeploymentRef {
	return client.DeploymentRef{Organization: ids.Organization, Workspace: ids.Workspace, ID: id}
}

func (m *deploymentModel) newDeployment() client.NewDeployment {
	autoDeploy := m.AutoDeploy.ValueBool()
	return client.NewDeployment{
		Name:       m.Name.ValueString(),
		Branch:     stringPointer(m.Branch),
		AutoDeploy: &autoDeploy,
	}
}

// set copies the details, but keeps the parents as configured, which may be
// slugs.
func (m *deploymentModel) set(d *client.DeploymentDetails) {
	m.ID = types.StringValue(d.Deployment.ID)
	m.Name = types.StringValue(d.Deployment.Name)
	m.Branch = types.StringValue(d.Tracker.Branch)
	m.AutoDeploy = types.BoolValue(d.Tracker.AutoDeploy)
	m.CreatedAt = types.StringValue(d.CreatedAt)
}

// update renames the deployment and changes its tracker where the plan
// differs from the state, and moves each change it made into the state, so
// that a failed step leaves the state at what the platform has.
func (m *deploymentModel) update(ctx context.Context, c *client.Client, ref client.DeploymentRef, state *deploymentModel) error {
	if !m.Name.Equal(state.Name) {
		if _, err := c.UpdateDeployment(ctx, ref, client.DeploymentPatch{Name: stringPointer(m.Name)}); err != nil {
			return fmt.Errorf("renaming: %w", err)
		}
		state.Name = m.Name
	}
	if !m.Branch.Equal(state.Branch) || !m.AutoDeploy.Equal(state.AutoDeploy) {
		tracker := client.Tracker{Branch: m.Branch.ValueString(), AutoDeploy: m.AutoDeploy.ValueBool()}
		if _, err := c.ChangeDeploymentTracker(ctx, ref, tracker); err != nil {
			return fmt.Errorf("changing the tracker: %w", err)
		}
		state.Branch = m.Branch
		state.AutoDeploy = m.AutoDeploy
	}
	return nil
}

func (r *deploymentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan deploymentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, plan.parents(), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	d, err := r.client.CreateDeployment(ctx, ids.Organization, ids.Workspace, plan.newDeployment())
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the deployment", err.Error())
		return
	}
	details, err := r.client.ReadDeploymentDetails(ctx, deploymentRef(ids, d.ID))
	if err != nil {
		plan.ID = types.StringValue(d.ID)
		plan.CreatedAt = types.StringNull()
		resp.Diagnostics.AddError("Cannot read the new deployment", err.Error())
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	plan.set(details)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deploymentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state deploymentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	details, ok := readDeployment(ctx, r.client, &state, req, resp)
	if !ok {
		return
	}
	state.set(details)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// readDeployment reads a deployment's details, and removes the resource
// from the state when it is gone.
func readDeployment(ctx context.Context, c *client.Client, m *deploymentModel, req resource.ReadRequest, resp *resource.ReadResponse) (*client.DeploymentDetails, bool) {
	ref, diags := m.ref(ctx, c, req.Private)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return nil, false
	}
	details, err := c.ReadDeploymentDetails(ctx, ref)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return nil, false
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the deployment", err.Error())
		return nil, false
	}
	return details, true
}

func (r *deploymentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state deploymentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ref, diags := state.ref(ctx, r.client, req.Private)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := plan.update(ctx, r.client, ref, &state); err != nil {
		resp.Diagnostics.AddError("Cannot update the deployment", err.Error())
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *deploymentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state deploymentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ref, diags := state.ref(ctx, r.client, req.Private)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteDeployment(ctx, ref)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the deployment", err.Error())
	}
}

// ImportState takes "<organization>/<workspace>/<deployment>", the first two
// each an ID or a slug, and stores IDs.
func (r *deploymentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(importDeployment(ctx, r.client, req.ID, hostingCustomer, resp)...)
}

// importDeployment resolves an import ID, checks that the deployment belongs
// to the workspace and has the expected hosting, and stores the IDs.
func importDeployment(ctx context.Context, c *client.Client, id, hosting string, resp *resource.ImportStateResponse) diag.Diagnostics {
	var diags diag.Diagnostics
	parts, err := splitImportID(id, "organization", "workspace", "deployment")
	if err != nil {
		diags.AddError("Invalid import ID", err.Error())
		return diags
	}
	ws, err := c.ReadWorkspace(ctx, parts[0], parts[1])
	if err != nil {
		diags.AddError("Cannot import the deployment", err.Error())
		return diags
	}
	// The platform finds a deployment only under its own workspace.
	details, err := c.ReadDeploymentDetails(ctx, client.DeploymentRef{Organization: ws.OrgID, Workspace: ws.ID, ID: parts[2]})
	if err != nil {
		diags.AddError("Cannot import the deployment", err.Error())
		return diags
	}
	if details.Hosting != hosting {
		other := "tenzir_hosted_deployment"
		if details.Hosting == hostingCustomer {
			other = "tenzir_deployment"
		}
		diags.AddError("Cannot import the deployment",
			fmt.Sprintf("Deployment %s is hosted by %s; import it as %s.", parts[2], details.Hosting, other))
		return diags
	}
	diags.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), ws.OrgID)...)
	diags.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), ws.ID)...)
	diags.Append(resp.State.SetAttribute(ctx, path.Root("id"), details.Deployment.ID)...)
	return diags
}
