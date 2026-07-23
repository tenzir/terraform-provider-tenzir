package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// Ensure TenzirProvider satisfies the provider interfaces.
var _ provider.Provider = &TenzirProvider{}
var _ provider.ProviderWithFunctions = &TenzirProvider{}

// TenzirProvider defines the provider implementation.
type TenzirProvider struct {
	// version is "dev" for local builds, or the release version for
	// binaries distributed via the registry.
	version string
}

// TenzirProviderModel maps the provider configuration block to Go values.
type TenzirProviderModel struct {
	Endpoint       types.String `tfsdk:"endpoint"`
	IDToken        types.String `tfsdk:"id_token"`
	ServiceToken   types.String `tfsdk:"service_token"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Stage          types.String `tfsdk:"stage"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &TenzirProvider{version: version}
	}
}

func (p *TenzirProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tenzir"
	resp.Version = p.version
}

func (p *TenzirProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage resources in the Tenzir Platform.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Description: "Base URL of the Tenzir Platform user API. " +
					"Can also be set via the TENZIR_PLATFORM_ENDPOINT environment variable.",
				Optional: true,
			},
			"id_token": schema.StringAttribute{
				Description: "OIDC ID token used to authenticate against the Tenzir Platform " +
					"(user-identity credentials; required for managing organizations). " +
					"The provider exchanges it for short-lived, workspace-scoped API keys as needed. " +
					"Can also be set via the TENZIR_PLATFORM_ID_TOKEN environment variable. " +
					"When neither this nor `service_token` is set, the provider falls back to " +
					"running `tzctl auth token` (if it is on PATH) and then to the id_token cached by " +
					"`tenzir-platform login` (see the `stage` attribute). " +
					"Exactly one of `id_token` and `service_token` must be set.",
				Optional:  true,
				Sensitive: true,
			},
			"service_token": schema.StringAttribute{
				Description: "Org-scoped service token (`tzst_...`) used to authenticate against " +
					"the Tenzir Platform. Manages workspaces and nodes within the token's " +
					"organization; cannot create organizations. " +
					"Can also be set via the TENZIR_PLATFORM_SERVICE_TOKEN environment variable. " +
					"Exactly one of `id_token` and `service_token` must be set.",
				Optional:  true,
				Sensitive: true,
			},
			"organization_id": schema.StringAttribute{
				Description: "When set together with `id_token`, the token is treated as a " +
					"federated workload identity token (e.g. a CI-issued OIDC token) and " +
					"exchanged against this organization's trusted-issuer configuration " +
					"instead of the platform's login IdP. " +
					"Can also be set via the TENZIR_PLATFORM_ORGANIZATION_ID environment variable. " +
					"Conflicts with `service_token`.",
				Optional: true,
			},
			"stage": schema.StringAttribute{
				Description: "Selects which `tenzir-platform` CLI login cache to read when no " +
					"`id_token` or `service_token` is set explicitly. The provider falls back to " +
					"the id_token cached by `tenzir-platform login` at " +
					"`${XDG_CACHE_HOME:-~/.cache}/tenzir-platform/<stage>/id_token`. " +
					"Defaults to `prod`; can also be set via the " +
					"TENZIR_PLATFORM_CLI_STAGE_IDENTIFIER environment variable.",
				Optional: true,
			},
		},
	}
}

func (p *TenzirProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config TenzirProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values take precedence over environment variables.
	endpoint := os.Getenv("TENZIR_PLATFORM_ENDPOINT")
	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}
	idToken := os.Getenv("TENZIR_PLATFORM_ID_TOKEN")
	if !config.IDToken.IsNull() {
		idToken = config.IDToken.ValueString()
	}
	serviceToken := os.Getenv("TENZIR_PLATFORM_SERVICE_TOKEN")
	if !config.ServiceToken.IsNull() {
		serviceToken = config.ServiceToken.ValueString()
	}
	organizationID := os.Getenv("TENZIR_PLATFORM_ORGANIZATION_ID")
	if !config.OrganizationID.IsNull() {
		organizationID = config.OrganizationID.ValueString()
	}
	stage := os.Getenv("TENZIR_PLATFORM_CLI_STAGE_IDENTIFIER")
	if !config.Stage.IsNull() {
		stage = config.Stage.ValueString()
	}
	if stage == "" {
		stage = "prod"
	}

	// When no explicit credentials are configured, fall back to a locally
	// available id_token so a CLI login is enough to run the provider locally.
	// First try the `tzctl auth token` command (a live fetch that can refresh
	// the token), then the id_token cached by `tenzir-platform login`.
	cachePath := cliTokenPath(stage)
	var tzctlErr string
	if idToken == "" && serviceToken == "" {
		if tok, err := tzctlToken(ctx, endpoint); err == nil {
			idToken = tok
		} else if err != errTzctlNotFound {
			tzctlErr = err.Error()
		}
	}
	if idToken == "" && serviceToken == "" {
		if tok, err := os.ReadFile(cachePath); err == nil {
			idToken = strings.TrimSpace(string(tok))
		}
	}

	if endpoint == "" {
		resp.Diagnostics.AddError(
			"Missing Tenzir Platform endpoint",
			"Set the `endpoint` provider attribute or the TENZIR_PLATFORM_ENDPOINT environment variable.",
		)
	}
	if idToken == "" && serviceToken == "" {
		detail := "Set either the `id_token` provider attribute (or TENZIR_PLATFORM_ID_TOKEN) for " +
			"user-identity credentials, or the `service_token` provider attribute (or " +
			"TENZIR_PLATFORM_SERVICE_TOKEN) for org-scoped credentials. As a fallback the " +
			"provider runs `tzctl auth token` (if on PATH) and then reads the id_token cached by " +
			"`tenzir-platform login` at " + cachePath + ", but neither yielded a token."
		if tzctlErr != "" {
			detail += "\n\n`tzctl auth token` failed: " + tzctlErr
		}
		resp.Diagnostics.AddError(
			"Missing Tenzir Platform credentials",
			detail,
		)
	}
	if idToken != "" && serviceToken != "" {
		resp.Diagnostics.AddError(
			"Conflicting Tenzir Platform credentials",
			"Both an ID token and a service token are configured (via attributes or environment "+
				"variables); set exactly one.",
		)
	}
	if organizationID != "" && serviceToken != "" {
		resp.Diagnostics.AddError(
			"Conflicting Tenzir Platform credentials",
			"`organization_id` selects federated authentication for an `id_token` and cannot be "+
				"combined with a service token.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var c *client.Client
	switch {
	case serviceToken != "":
		c = client.NewWithServiceToken(endpoint, serviceToken)
	case organizationID != "":
		c = client.NewFederated(endpoint, idToken, organizationID)
	default:
		c = client.New(endpoint, idToken)
	}
	// Made available to resources and data sources via req.ProviderData.
	resp.DataSourceData = c
	resp.ResourceData = c
}

// errTzctlNotFound signals that the `tzctl` binary is not on PATH, so the
// credential fallback should silently continue to the next source.
var errTzctlNotFound = errors.New("tzctl not found on PATH")

// tzctlToken runs `tzctl auth token` to obtain an id_token, returning
// errTzctlNotFound when the binary is not on PATH. Any other failure (non-zero
// exit, timeout) is returned with captured stderr for diagnostics. When
// endpoint is non-empty it is passed via `--api-endpoint` so tzctl targets the
// same platform as the provider.
func tzctlToken(ctx context.Context, endpoint string) (string, error) {
	path, err := exec.LookPath("tzctl")
	if err != nil {
		return "", errTzctlNotFound
	}
	// Bound the call so a hung `tzctl` cannot block `terraform plan`.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"auth", "token"}
	if endpoint != "" {
		args = append(args, "--api-endpoint", endpoint)
	}
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", errors.New("`tzctl token` returned an empty token")
	}
	return tok, nil
}

// cliTokenPath returns the location where the `tenzir-platform` CLI caches the
// id_token for the given stage, mirroring the CLI's own resolution
// (${XDG_CACHE_HOME:-~/.cache}/tenzir-platform/<stage>/id_token).
func cliTokenPath(stage string) string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Leave home empty; the resulting path simply won't exist and the
			// read falls through to the missing-credentials diagnostic.
			home = ""
		}
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "tenzir-platform", stage, "id_token")
}

func (p *TenzirProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewOrganizationResource,
		NewWorkspaceResource,
		NewNodeResource,
		NewFederatedCredentialResource,
		NewServiceTokenResource,
		NewOrganizationMemberResource,
		NewLibrarySourceResource,
		NewSecretResource,
		NewSecretStoreResource,
		NewWorkspaceAuthRuleResource,
		NewAlertResource,
	}
}

func (p *TenzirProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
		NewWorkspaceDataSource,
		NewNodesDataSource,
		NewOrganizationMembersDataSource,
	}
}

func (p *TenzirProvider) Functions(ctx context.Context) []func() function.Function {
	return nil
}

// resourceClient extracts the shared API client handed to resources by
// Configure, adding the error diagnostic on type mismatch.
func resourceClient(providerData any, diagAdd func(string, string)) *client.Client {
	if providerData == nil {
		// Terraform calls Configure on resources before the provider is
		// configured during validation; ProviderData is nil then.
		return nil
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diagAdd(
			"Unexpected Provider Data Type",
			"Expected *client.Client. This is a bug in the provider.",
		)
		return nil
	}
	return c
}
