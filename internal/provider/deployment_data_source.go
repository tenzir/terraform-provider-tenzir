package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &deploymentDataSource{}

func NewDeploymentDataSource() datasource.DataSource {
	return &deploymentDataSource{}
}

type deploymentDataSource struct {
	client *client.Client
}

type deploymentDataModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Name         types.String `tfsdk:"name"`
	Hosting      types.String `tfsdk:"hosting"`
	Size         types.String `tfsdk:"size"`
	Region       types.String `tfsdk:"region"`
	Branch       types.String `tfsdk:"branch"`
	AutoDeploy   types.Bool   `tfsdk:"auto_deploy"`
	Reachable    types.Bool   `tfsdk:"reachable"`
	Version      types.String `tfsdk:"version"`
	Source       types.String `tfsdk:"source"`
	Runtime      types.String `tfsdk:"runtime"`
	LastSeen     types.String `tfsdk:"last_seen"`
	UpgradeImage types.String `tfsdk:"upgrade_image"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (d *deploymentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment"
}

func (d *deploymentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: description, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A deployment as it is right now: whether it is connected, and what it last " +
			"reported. Needs `Deployment.read` and `Deployment.readTracker`.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{MarkdownDescription: "The deployment ID.", Required: true},
			"workspace_id": computed("The ID of its workspace."),
			"name":         computed("The display name."),
			"hosting":      computed("Who runs it: `tenzir` or `customer`."),
			"size":         computed("The size it runs as; null unless Tenzir runs it."),
			"region":       computed("The cloud region it runs in; null unless Tenzir runs it."),
			"branch":       computed("The branch it follows."),
			"auto_deploy":  schema.BoolAttribute{MarkdownDescription: "Whether new commits deploy on their own.", Computed: true},
			"reachable":    schema.BoolAttribute{MarkdownDescription: "Whether it is connected to the platform.", Computed: true},
			"version":      computed("The version it reported when it last connected."),
			"source":       computed("The source tag of the build it ran when it last connected, such as `src-<hash>`."),
			"runtime": computed("What ran it when it last connected: `kubernetes`, `ecs`, `compose`, `docker`, " +
				"`systemd` or `unmanaged`."),
			"last_seen": computed("When it was last connected."),
			"upgrade_image": computed("The image of the platform's version, when it runs an older one; null when " +
				"it is current or the platform cannot tell."),
			"created_at": computed("When it was created."),
		},
	}
}

func (d *deploymentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *deploymentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config deploymentDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	details, err := d.client.ReadDeploymentDetails(ctx, config.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the deployment", err.Error())
		return
	}
	upgrade := types.StringNull()
	if details.Upgrade != nil && details.Upgrade.Tag == "Available" {
		upgrade = types.StringValue(details.Upgrade.Image)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &deploymentDataModel{
		ID:           types.StringValue(details.Deployment.ID),
		WorkspaceID:  types.StringValue(details.Deployment.WorkspaceID),
		Name:         types.StringValue(details.Deployment.Name),
		Hosting:      types.StringValue(details.Hosting),
		Size:         optionalString(details.Size),
		Region:       optionalString(details.Region),
		Branch:       types.StringValue(details.Tracker.Branch),
		AutoDeploy:   types.BoolValue(details.Tracker.AutoDeploy),
		Reachable:    types.BoolValue(details.Reachable),
		Version:      optionalString(details.Deployment.Version),
		Source:       optionalString(details.Source),
		Runtime:      optionalString(details.Runtime),
		LastSeen:     optionalString(details.Deployment.LastSeen),
		UpgradeImage: upgrade,
		CreatedAt:    types.StringValue(details.CreatedAt),
	})...)
}
