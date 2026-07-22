package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &AlertResource{}
var _ resource.ResourceWithImportState = &AlertResource{}

func NewAlertResource() resource.Resource {
	return &AlertResource{}
}

// AlertResource manages a node-offline alert in a Tenzir Platform
// workspace.
type AlertResource struct {
	client *client.Client
}

type AlertResourceModel struct {
	ID          types.String         `tfsdk:"id"`
	WorkspaceID types.String         `tfsdk:"workspace_id"`
	NodeID      types.String         `tfsdk:"node_id"`
	Duration    types.Int64          `tfsdk:"duration"`
	WebhookURL  types.String         `tfsdk:"webhook_url"`
	WebhookBody jsontypes.Normalized `tfsdk:"webhook_body"`
}

func (r *AlertResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert"
}

func (r *AlertResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A node-offline alert in a Tenzir Platform workspace. When the node has " +
			"been disconnected for `duration` seconds, the platform sends `webhook_body` " +
			"as an HTTP POST request to `webhook_url`.\n\n" +
			"The platform supports at most one alert per node. It also has no update " +
			"operation for alerts, so changing any attribute replaces the alert.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the alert.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Description: "Identifier of the workspace the alert belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"node_id": schema.StringAttribute{
				Description: "Identifier of the node whose connectivity is monitored.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"duration": schema.Int64Attribute{
				Description: "How long the node must be disconnected before the alert " +
					"fires, in seconds.",
				Required: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"webhook_url": schema.StringAttribute{
				Description: "URL the alert webhook is POSTed to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"webhook_body": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Description: "Body of the webhook request. Must be a valid JSON document; " +
					"prefer `jsonencode()` over a raw string.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *AlertResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *AlertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.AddAlert(ctx, plan.WorkspaceID.ValueString(), client.Alert{
		NodeID:      plan.NodeID.ValueString(),
		Duration:    plan.Duration.ValueInt64(),
		WebhookURL:  plan.WebhookURL.ValueString(),
		WebhookBody: plan.WebhookBody.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating alert", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AlertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	alert, err := r.client.GetAlert(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading alert", err.Error())
		return
	}
	if alert == nil {
		// The alert (or its whole workspace) was deleted outside of
		// Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.NodeID = types.StringValue(alert.NodeID)
	state.Duration = types.Int64Value(alert.Duration)
	state.WebhookURL = types.StringValue(alert.WebhookURL)
	state.WebhookBody = jsontypes.NewNormalizedValue(alert.WebhookBody)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AlertResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Unreachable: every attribute carries a RequiresReplace plan modifier,
	// since the platform has no update endpoint for alerts.
	resp.Diagnostics.AddError(
		"Alerts cannot be updated in place",
		"This is a bug in the provider: all attribute changes should have planned a replacement.",
	)
}

func (r *AlertResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteAlert(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting alert", err.Error())
	}
}

func (r *AlertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Alerts are addressed by workspace and alert id:
	// `terraform import tenzir_alert.example <workspace-id>/<alert-id>`
	workspaceID, alertID, ok := strings.Cut(req.ID, "/")
	if !ok || workspaceID == "" || alertID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <workspace-id>/<alert-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), alertID)...)
}
