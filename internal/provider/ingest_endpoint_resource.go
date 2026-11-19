package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &ingestEndpointResource{}
	_ resource.ResourceWithImportState = &ingestEndpointResource{}
	_ resource.ResourceWithModifyPlan  = &ingestEndpointResource{}
)

func NewIngestEndpointResource() resource.Resource {
	return &ingestEndpointResource{}
}

type ingestEndpointResource struct {
	client *client.Client
}

type ingestEndpointModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	ReadableBy     types.Object `tfsdk:"readable_by"`
	URL            types.String `tfsdk:"url"`
	Token          types.String `tfsdk:"token"`
}

func (r *ingestEndpointResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ingest_endpoint"
}

func (r *ingestEndpointResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An ingest endpoint: a URL that turns every request sent to it with its token into " +
			"one event, for pipelines on the deployments of the organization that may read it. Destroying it " +
			"drops the events it buffers.\n\n" +
			"Needs `Organization.createIngestEndpoint` and `IngestEndpoint.read`, `IngestEndpoint.readToken`, " +
			"`IngestEndpoint.update` and `IngestEndpoint.delete`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The endpoint ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The ID or slug of the organization.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name pipelines refer to the endpoint by: 3 to 21 lowercase letters, " +
					"digits and single dashes. Changing it replaces the endpoint.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the endpoint receives.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"readable_by": readableBySchema("endpoint"),
			"url": schema.StringAttribute{
				MarkdownDescription: "Where senders post events, at any path. Null when the platform has no host for ingest endpoints.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "The token senders authenticate with.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ingestEndpointResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (r *ingestEndpointResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	planParents(ctx, r.client, req, resp, false)
}

func (m *ingestEndpointModel) set(ctx context.Context, ep *client.IngestEndpoint) error {
	m.ID = types.StringValue(ep.ID)
	m.Name = types.StringValue(ep.Name)
	m.Description = types.StringValue(ep.Description)
	m.URL = optionalString(ep.URL)
	readers, diags := readableByFromAPI(ctx, ep.ReadableBy, m.ReadableBy)
	if diags.HasError() {
		return diagsError(diags)
	}
	m.ReadableBy = readers
	return nil
}

func (r *ingestEndpointResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ingestEndpointModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	readers, diags := readableByToAPI(ctx, plan.ReadableBy)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, ok := recordParents(ctx, r.client, organizationParent(plan.OrganizationID), resp.Private, &resp.Diagnostics)
	if !ok {
		return
	}
	ep, token, err := r.client.CreateIngestEndpoint(ctx, ids.Organization, client.NewIngestEndpoint{
		Name:        plan.Name.ValueString(),
		Description: stringPointer(plan.Description),
		ReadableBy:  &readers,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the ingest endpoint", err.Error())
		return
	}
	if err := plan.set(ctx, ep); err != nil {
		resp.Diagnostics.AddError("Cannot read the endpoint's readers", err.Error())
	}
	plan.Token = types.StringValue(token)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ingestEndpointResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ingestEndpointModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ep, err := r.client.ReadIngestEndpoint(ctx, ids.Organization, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the ingest endpoint", err.Error())
		return
	}
	token, err := r.client.ReadIngestToken(ctx, ids.Organization, ep.ID)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the ingest endpoint's token", err.Error())
		return
	}
	if err := state.set(ctx, ep); err != nil {
		resp.Diagnostics.AddError("Cannot read the endpoint's readers", err.Error())
		return
	}
	state.Token = types.StringValue(token)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ingestEndpointResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ingestEndpointModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	readers, diags := readableByToAPI(ctx, plan.ReadableBy)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ep, err := r.client.UpdateIngestEndpoint(ctx, ids.Organization, state.ID.ValueString(), client.IngestEndpointPatch{
		Description: stringPointer(plan.Description),
		ReadableBy:  &readers,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the ingest endpoint", err.Error())
		return
	}
	if err := plan.set(ctx, ep); err != nil {
		resp.Diagnostics.AddError("Cannot read the endpoint's readers", err.Error())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ingestEndpointResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ingestEndpointModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids, diags := knownParents(ctx, r.client, req.Private, organizationParent(state.OrganizationID))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteIngestEndpoint(ctx, ids.Organization, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the ingest endpoint", err.Error())
	}
}

// ImportState takes "<organization>/<endpoint>", the organization an ID or
// a slug.
func (r *ingestEndpointResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, "organization", "endpoint")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, organizationPath, parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
