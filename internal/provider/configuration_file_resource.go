package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &configurationFileResource{}
	_ resource.ResourceWithImportState = &configurationFileResource{}
	_ resource.ResourceWithModifyPlan  = &configurationFileResource{}
)

func NewConfigurationFileResource() resource.Resource {
	return &configurationFileResource{}
}

type configurationFileResource struct {
	client *client.Client
}

type configurationFileModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	WorkspaceID    types.String `tfsdk:"workspace_id"`
	Branch         types.String `tfsdk:"branch"`
	Path           types.String `tfsdk:"path"`
	Content        types.String `tfsdk:"content"`
	CommitMessage  types.String `tfsdk:"commit_message"`
	Commit         types.String `tfsdk:"commit"`
}

func (r *configurationFileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configuration_file"
}

func (r *configurationFileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "One text file on one branch of a workspace's configuration repository, such as " +
			"a pipeline at `pipelines/<name>.tql`. Deployments that follow the branch run what it declares.\n\n" +
			"Every create, update and delete is one commit by the credential's user, which the provider makes " +
			"against the branch tip it read the file at. When the branch moves in between, it reads the file " +
			"again and retries, up to three attempts. Writes to the same branch wait for each other within one " +
			"Terraform run. The provider does not touch configuration drafts, so edits that someone has not " +
			"published in the web UI stay as they are.\n\n" +
			"Creating a file that exists with other content fails; import it instead. Deleting a file that is " +
			"gone succeeds. Package files (`package.yaml`, `package.lock`, `src/`) are refused, because " +
			"deployments do not run them yet. After a write, the provider warns when the branch holds " +
			"something no deployment would take.\n\n" +
			"Needs a user's credential and `Workspace.read`, `Workspace.readFileTree`, `Workspace.readFile`, " +
			"`Workspace.readConfiguredPipelines` and `Workspace.commitChanges`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "`<organization ID>/<workspace ID>/<branch>:<path>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the workspace whose repository holds the file.",
				Required:            true,
			},
			"branch": schema.StringAttribute{
				MarkdownDescription: "The branch that holds the file. Defaults to `main`, which every new workspace " +
					"has. The branch must exist.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("main"),
				PlanModifiers: replace,
			},
			"path": schema.StringAttribute{
				MarkdownDescription: "The file's path in the repository, such as `pipelines/ingest.tql` or `tenzir.yaml`.",
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{configurationPathValidator{}},
			},
			"content": schema.StringAttribute{
				MarkdownDescription: "The file's text. A pipeline file is TQL, optionally after YAML frontmatter " +
					"between `---` lines that sets `name` or `disabled`.",
				Required: true,
			},
			"commit_message": schema.StringAttribute{
				MarkdownDescription: "The message of the commits that create and update the file. Defaults to " +
					"`terraform: create <path>` or `terraform: update <path>`; deleting says `terraform: delete <path>`. " +
					"Changing only the message commits nothing.",
				Optional: true,
			},
			"commit": schema.StringAttribute{
				MarkdownDescription: "The commit that holds the content: the one the last write made, or the branch " +
					"tip when the content was already there. It does not follow later commits to other files.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *configurationFileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan compares the parents, and plans a new commit only when the
// content changes.
func (r *configurationFileResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, true)
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state configurationFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || plan.Content.Equal(state.Content) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("commit"), types.StringUnknown())...)
}

func (m *configurationFileModel) parents() parentRefs {
	return parentRefs{Organization: m.OrganizationID, Workspace: m.WorkspaceID}
}

// target names the workspace by ID, so that writes to one branch lock the
// same key however the configuration names the workspace.
func (m *configurationFileModel) target(ids parentIDs) branchTarget {
	return branchTarget{Organization: ids.Organization, WorkspaceID: ids.Workspace, Branch: m.Branch.ValueString()}
}

func (m *configurationFileModel) message(verb string) string {
	if verb != "delete" && m.CommitMessage.ValueString() != "" {
		return m.CommitMessage.ValueString()
	}
	return "terraform: " + verb + " " + m.Path.ValueString()
}

func configurationFileID(t branchTarget, p string) string {
	return t.Organization + "/" + t.WorkspaceID + "/" + t.Branch + ":" + p
}

// write publishes the planned content and records the commit.
func (r *configurationFileResource) write(ctx context.Context, plan *configurationFileModel, ids parentIDs, verb string, diags *diag.Diagnostics) bool {
	t := plan.target(ids)
	content := plan.Content.ValueString()
	commit, err := publishFile(ctx, r.client, t, fileWrite{
		Path:    plan.Path.ValueString(),
		Content: &content,
		Message: plan.message(verb),
		Create:  verb == "create",
	})
	if err != nil {
		diags.AddError("Cannot "+verb+" "+plan.Path.ValueString(), err.Error())
		return false
	}
	plan.ID = types.StringValue(configurationFileID(t, plan.Path.ValueString()))
	plan.Commit = types.StringValue(commit)
	warnAboutBranch(ctx, r.client, t, diags)
	return true
}

// warnAboutBranch passes on why no deployment would take the branch.
func warnAboutBranch(ctx context.Context, c *client.Client, t branchTarget, diags *diag.Diagnostics) {
	pipelines, err := c.ReadConfiguredPipelines(ctx, t.Organization, t.WorkspaceID, t.Branch)
	if err != nil || pipelines.Problem == nil {
		return
	}
	diags.AddWarning("Deployments refuse branch "+t.Branch,
		"The commit succeeded, but deployments that follow the branch keep their previous configuration: "+*pipelines.Problem)
}

func (r *configurationFileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan configurationFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, plan.parents(), resp.Private, &resp.Diagnostics)
	if !ok || !r.write(ctx, &plan, ids, "create", &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *configurationFileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state configurationFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, state.parents())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tip, content, err := readBranchFile(ctx, r.client, ids.Organization, ids.Workspace,
		state.Branch.ValueString(), state.Path.ValueString())
	if client.IsNotFound(err) || (err == nil && content == nil) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read "+state.Path.ValueString(), err.Error())
		return
	}
	state.Content = types.StringValue(*content)
	if state.Commit.IsNull() {
		state.Commit = types.StringValue(tip)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *configurationFileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state configurationFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Content.Equal(state.Content) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, state.parents())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() || !r.write(ctx, &plan, ids, "update", &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *configurationFileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state configurationFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, state.parents())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.ReadWorkspace(ctx, ids.Organization, ids.Workspace)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the workspace", err.Error())
		return
	}
	_, err = publishFile(ctx, r.client, state.target(ids), fileWrite{Path: state.Path.ValueString(), Message: state.message("delete")})
	if err != nil {
		resp.Diagnostics.AddError("Cannot delete "+state.Path.ValueString(), err.Error())
	}
}

// ImportState takes "<organization>/<workspace>/<branch>:<path>", the first
// two each an ID or a slug. Git refuses a colon in a branch name, so the
// first colon ends the branch, which may contain slashes.
func (r *configurationFileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, workspace, branch, p, err := splitConfigurationFileID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	ws, err := r.client.ReadWorkspace(ctx, org, workspace)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import "+p, err.Error())
		return
	}
	t := branchTarget{Organization: ws.OrgID, WorkspaceID: ws.ID, Branch: branch}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), configurationFileID(t, p))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), ws.OrgID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), ws.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch"), branch)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), p)...)
}

func splitConfigurationFileID(id string) (org, workspace, branch, p string, err error) {
	const form = "<organization>/<workspace>/<branch>:<path>"
	head, p, ok := strings.Cut(id, ":")
	parts := strings.SplitN(head, "/", 3)
	if !ok || len(parts) != 3 {
		return "", "", "", "", fmt.Errorf("expected an import ID of the form %s, got %q", form, id)
	}
	for i, name := range []string{"organization", "workspace", "branch"} {
		if parts[i] == "" {
			return "", "", "", "", fmt.Errorf("the %s in import ID %q is empty", name, id)
		}
	}
	if p == "" {
		return "", "", "", "", fmt.Errorf("the path in import ID %q is empty", id)
	}
	return parts[0], parts[1], parts[2], p, nil
}

// configurationPathValidator applies checkConfigurationPath at plan time.
type configurationPathValidator struct{}

func (configurationPathValidator) Description(context.Context) string {
	return "a path the platform saves and deployments take"
}

func (v configurationPathValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (configurationPathValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := checkConfigurationPath(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid configuration file path", err.Error())
	}
}
