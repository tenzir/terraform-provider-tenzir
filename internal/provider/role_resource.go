package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.ResourceWithConfigure   = &roleResource{}
	_ resource.ResourceWithImportState = &roleResource{}
	_ resource.ResourceWithModifyPlan  = &roleResource{}
)

var roleColors = []string{"neutral", "blue", "green", "lightblue", "purple", "pink", "orange", "yellow", "red"}

func NewRoleResource() resource.Resource {
	return &roleResource{}
}

type roleResource struct {
	client *client.Client
}

type roleModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Label          types.String `tfsdk:"label"`
	Color          types.String `tfsdk:"color"`
	Description    types.String `tfsdk:"description"`
	Policy         PolicyValue  `tfsdk:"policy"`
}

func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func policyDescription(subject string) string {
	return subject + " as JSON: an array of `[constraints, actions]` pairs, such as " +
		"`[[{\"target\":{\"_tag\":\"Workspace\"},\"relationships\":[]},[\"Workspace.read\"]]]`. " +
		"The platform normalizes policies, merging grants with equal constraints and dropping " +
		"actions a broader grant covers; a policy that normalizes alike shows no difference. " +
		"Use `jsonencode` to write one."
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom role of an organization. Assign it with `tenzir_role_assignment`.\n\n" +
			"Needs `Organization.createRole` and `OrganizationRole.read`, `OrganizationRole.update` and " +
			"`OrganizationRole.delete`. The platform refuses a change that leaves no member able to " +
			"restore access to the organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The role ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "The display name.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "The display color: one of `neutral`, `blue`, `green`, `lightblue`, `purple`, " +
					"`pink`, `orange`, `yellow` or `red`. Defaults to `neutral`.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("neutral"),
				Validators: []validator.String{stringvalidator.OneOf(roleColors...)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the role is for.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"policy": schema.StringAttribute{
				MarkdownDescription: policyDescription("What the role grants,"),
				Required:            true,
				CustomType:          PolicyType{},
			},
		},
	}
}

func (r *roleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *roleResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, false)
}

func (m *roleModel) set(role *client.Role) {
	m.ID = types.StringValue(role.ID)
	m.Label = types.StringValue(role.Label)
	m.Color = types.StringValue(role.Color)
	m.Description = types.StringValue("")
	if role.Description != nil {
		m.Description = types.StringValue(*role.Description)
	}
	m.Policy = NewPolicyValue(string(role.Policy))
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, organizationParent(plan.OrganizationID), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	role, err := r.client.CreateRole(ctx, ids.Organization, client.NewRole{
		Label:       plan.Label.ValueString(),
		Color:       plan.Color.ValueString(),
		Description: stringPointer(plan.Description),
		Policy:      json.RawMessage(plan.Policy.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the role", err.Error())
		return
	}
	plan.set(role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	role, err := r.client.ReadRole(ctx, ids.Organization, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the role", err.Error())
		return
	}
	if role.Tag != "Custom" {
		resp.Diagnostics.AddError("Not a custom role", "Role "+role.ID+" is built in; only custom roles can be managed.")
		return
	}
	state.set(role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var patch client.RolePatch
	if !plan.Label.Equal(state.Label) {
		patch.Label = stringPointer(plan.Label)
	}
	if !plan.Color.Equal(state.Color) {
		patch.Color = stringPointer(plan.Color)
	}
	if !plan.Description.Equal(state.Description) {
		patch.Description = stringPointer(plan.Description)
	}
	if !plan.Policy.Equal(state.Policy) {
		patch.Policy = json.RawMessage(plan.Policy.ValueString())
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	role, err := r.client.UpdateRole(ctx, ids.Organization, state.ID.ValueString(), patch)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the role", err.Error())
		return
	}
	plan.set(role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRole(ctx, ids.Organization, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the role", err.Error())
	}
}

// ImportState takes "<organization>/<role>", where the organization is an ID
// or a slug.
func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "organization", "role")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	org, err := r.client.ReadOrganization(ctx, parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Cannot import the role", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), org.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
