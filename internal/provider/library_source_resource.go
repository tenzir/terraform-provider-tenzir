package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ resource.Resource = &LibrarySourceResource{}
var _ resource.ResourceWithImportState = &LibrarySourceResource{}

func NewLibrarySourceResource() resource.Resource {
	return &LibrarySourceResource{}
}

// LibrarySourceResource manages an organization-level library source, a
// GitHub repository from which packages are offered in the organization's
// workspace libraries.
type LibrarySourceResource struct {
	client *client.Client
}

type LibrarySourceResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	OrganizationID       types.String `tfsdk:"organization_id"`
	Name                 types.String `tfsdk:"name"`
	GitHubURL            types.String `tfsdk:"github_url"`
	BranchOrRef          types.String `tfsdk:"branch_or_ref"`
	CredentialSecretName types.String `tfsdk:"credential_secret_name"`
}

func (r *LibrarySourceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_library_source"
}

func (r *LibrarySourceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An organization-level library source: a GitHub repository from which " +
			"packages are offered in the organization's workspace libraries, in addition " +
			"to the platform-configured default sources. Requires org-admin credentials. " +
			"All settings are changed in place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier of the library source.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Description: "Identifier of the organization the library source belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the library source.",
				Required:    true,
			},
			"github_url": schema.StringAttribute{
				Description: "URL of the GitHub repository, of the form " +
					"`https://github.com/<owner>/<repo>`.",
				Required: true,
			},
			"branch_or_ref": schema.StringAttribute{
				Description: "Git branch or ref to fetch packages from. Defaults to `main`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("main"),
			},
			"credential_secret_name": schema.StringAttribute{
				Description: "Name of an org-scoped platform secret holding the access token " +
					"for a private repository. Unset means a public repository. The token " +
					"itself is stored separately and never passes through this resource.",
				Optional: true,
			},
		},
	}
}

func (r *LibrarySourceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = resourceClient(req.ProviderData, resp.Diagnostics.AddError)
}

func (r *LibrarySourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LibrarySourceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.AddLibrarySource(ctx, plan.OrganizationID.ValueString(), client.LibrarySource{
		Name:                 plan.Name.ValueString(),
		GitHubURL:            plan.GitHubURL.ValueString(),
		BranchOrRef:          plan.BranchOrRef.ValueString(),
		CredentialSecretName: plan.CredentialSecretName.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating library source", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *LibrarySourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LibrarySourceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	source, err := r.client.GetLibrarySource(ctx,
		state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading library source", err.Error())
		return
	}
	if source == nil {
		// The source (or its whole organization) was deleted outside of
		// Terraform; plan a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(source.Name)
	state.GitHubURL = types.StringValue(source.GitHubURL)
	state.BranchOrRef = types.StringValue(source.BranchOrRef)
	state.CredentialSecretName = types.StringPointerValue(source.CredentialSecretName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *LibrarySourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LibrarySourceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateLibrarySource(ctx, plan.OrganizationID.ValueString(), client.LibrarySource{
		ID:                   plan.ID.ValueString(),
		Name:                 plan.Name.ValueString(),
		GitHubURL:            plan.GitHubURL.ValueString(),
		BranchOrRef:          plan.BranchOrRef.ValueString(),
		CredentialSecretName: plan.CredentialSecretName.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error updating library source", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *LibrarySourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LibrarySourceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveLibrarySource(ctx,
		state.OrganizationID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting library source", err.Error())
	}
}

func (r *LibrarySourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Library sources are addressed by organization and source id:
	// `terraform import tenzir_library_source.example <organization-id>/<source-id>`
	organizationID, sourceID, ok := strings.Cut(req.ID, "/")
	if !ok || organizationID == "" || sourceID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <organization-id>/<source-id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), organizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), sourceID)...)
}
