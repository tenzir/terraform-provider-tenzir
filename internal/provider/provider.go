package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

const defaultPlatformURL = "https://platform.tenzir.dev"

var _ provider.Provider = &TenzirProvider{}

// TenzirProvider manages resources in the Tenzir Platform.
type TenzirProvider struct {
	version string
}

type providerModel struct {
	URL    types.String `tfsdk:"url"`
	APIKey types.String `tfsdk:"api_key"`
	Token  types.String `tfsdk:"token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &TenzirProvider{version: version}
	}
}

func (p *TenzirProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tenzir"
	resp.Version = p.version
}

func (p *TenzirProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage workspaces, access, secrets, ingest endpoints, deployments and their configuration in the Tenzir Platform.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				MarkdownDescription: "The platform address, which serves `/.well-known/tenzir-platform`. " +
					"Defaults to the `TENZIR_PLATFORM_URL` environment variable, or `" + defaultPlatformURL + "`.",
				Optional: true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "A user API key (`tnzu_…`), sent as `x-tenzir-api-key`. " +
					"Defaults to the `TENZIR_API_KEY` environment variable. Conflicts with `token`.",
				Optional:   true,
				Sensitive:  true,
				Validators: []validator.String{stringvalidator.ConflictsWith(path.MatchRoot("token"))},
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "An OIDC bearer token that the platform accepts. " +
					"Defaults to the `TENZIR_TOKEN` environment variable. Conflicts with `api_key`.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (p *TenzirProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for name, value := range map[string]types.String{"url": config.URL, "api_key": config.APIKey, "token": config.Token} {
		if value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider configuration",
				"The provider needs `"+name+"` before it can plan, so it cannot depend on a value that is only known after apply.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	url := setting(config.URL, "TENZIR_PLATFORM_URL")
	if url == "" {
		url = defaultPlatformURL
	}
	apiKey := setting(config.APIKey, "TENZIR_API_KEY")
	token := setting(config.Token, "TENZIR_TOKEN")
	if !config.APIKey.IsNull() && config.Token.IsNull() {
		token = ""
	}
	if !config.Token.IsNull() && config.APIKey.IsNull() {
		apiKey = ""
	}
	if apiKey == "" && token == "" {
		resp.Diagnostics.AddError("Missing credential",
			"Set `api_key` (or TENZIR_API_KEY) to a user API key, or `token` (or TENZIR_TOKEN) to a bearer token.")
		return
	}
	if apiKey != "" && token != "" {
		resp.Diagnostics.AddError("Conflicting credentials",
			"Both an API key and a token are set, through attributes or the environment. The platform refuses requests that carry both; set one.")
		return
	}

	c, err := client.New(ctx, client.Config{
		URL:       url,
		APIKey:    apiKey,
		Token:     token,
		UserAgent: "terraform-provider-tenzir/" + p.version,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot reach the Tenzir Platform", err.Error())
		return
	}
	resp.DataSourceData = c
	resp.ResourceData = c
}

// setting prefers the configured value over the environment variable.
func setting(value types.String, env string) string {
	if !value.IsNull() {
		return value.ValueString()
	}
	return os.Getenv(env)
}

func (p *TenzirProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewOrganizationResource,
		NewOrganizationMemberResource,
		NewWorkspaceResource,
		NewRoleResource,
		NewRoleAssignmentResource,
		NewAPIKeyResource,
		NewSecretResource,
		NewIngestEndpointResource,
		NewDeploymentResource,
		NewHostedDeploymentResource,
		NewDeploymentKeyResource,
		NewConfigurationFileResource,
	}
}

func (p *TenzirProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
		NewWorkspaceDataSource,
		NewPeopleDataSource,
		NewRolesDataSource,
		NewUserDataSource,
		NewDeploymentTypesDataSource,
		NewDeploymentImagesDataSource,
		NewDeploymentDataSource,
	}
}
