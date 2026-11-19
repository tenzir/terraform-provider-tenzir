package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// TestAccParentsBySlug names parents by slug, imports by ID, and switches
// between both forms, none of which may plan a change.
func TestAccParentsBySlug(t *testing.T) {
	workspace := testAccName("tf-acc-parents")
	other := testAccName("tf-acc-parents")
	name := "tf_acc_" + testAccName("parents")[7:]
	config := func(org, ws, secretWorkspace string) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %[3]q
}

resource "tenzir_workspace" "other" {
  organization_id = data.tenzir_organization.test.id
  slug            = %[4]q
}

locals {
  org = %[1]s
  ws  = %[2]s
}

resource "tenzir_role" "test" {
  organization_id = local.org
  label           = "tf-acc-parents"
  policy          = jsonencode([])
}

resource "tenzir_secret" "test" {
  organization_id = local.org
  workspace_id    = %[5]s
  name            = %[6]q
  value           = "v"
  depends_on      = [tenzir_workspace.test, tenzir_workspace.other]
}

resource "tenzir_deployment" "test" {
  organization_id = local.org
  workspace_id    = local.ws
  name            = "parents"
  depends_on      = [tenzir_workspace.test]
}

resource "tenzir_configuration_file" "test" {
  organization_id = local.org
  workspace_id    = local.ws
  path            = "pipelines/parents.tql"
  content         = "from {x: 1}\n"
  depends_on      = [tenzir_workspace.test]
}
`, org, ws, workspace, other, secretWorkspace, name)
	}
	const orgSlug, orgID = "data.tenzir_organization.test.slug", "data.tenzir_organization.test.id"
	wsSlug, wsID := fmt.Sprintf("%q", workspace), "tenzir_workspace.test.id"
	bySlug := config(orgSlug, wsSlug, "local.ws")
	byID := config(orgID, wsID, "local.ws")
	importBySlug := func(resourceName, id string) resource.TestStep {
		return resource.TestStep{
			Config:          bySlug,
			ResourceName:    resourceName,
			ImportState:     true,
			ImportStateKind: resource.ImportBlockWithID,
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[resourceName]
				if !ok {
					return "", fmt.Errorf("%s is not in state", resourceName)
				}
				org := s.RootModule().Resources["data.tenzir_organization.test"].Primary.Attributes["slug"]
				return strings.NewReplacer("{org}", org, "{id}", rs.Primary.ID).Replace(id), nil
			},
			ImportPlanChecks: resource.ImportPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
		}
	}
	emptyPlan := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{Config: bySlug},
			importBySlug("tenzir_role.test", "{org}/{id}"),
			importBySlug("tenzir_secret.test", "{org}/"+workspace+"/"+name),
			importBySlug("tenzir_deployment.test", "{org}/"+workspace+"/{id}"),
			importBySlug("tenzir_configuration_file.test", "{org}/"+workspace+"/main:pipelines/parents.tql"),
			{Config: byID, ConfigPlanChecks: emptyPlan},
			{Config: bySlug, ConfigPlanChecks: emptyPlan},
			{
				Config: config(orgSlug, wsSlug, fmt.Sprintf("%q", other)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("tenzir_secret.test", plancheck.ResourceActionReplace),
						plancheck.ExpectResourceAction("tenzir_deployment.test", plancheck.ResourceActionNoop),
					},
				},
			},
		},
	})
}

// TestAccParentRenamed renames a workspace outside Terraform, and gives its
// old slug to another one.
func TestAccParentRenamed(t *testing.T) {
	slug := testAccName("tf-acc-renamed")
	org := os.Getenv("TENZIR_TEST_ORGANIZATION")
	var c *client.Client
	var ws *client.Workspace
	config := func(workspace string) string {
		return fmt.Sprintf(`
resource "tenzir_deployment" "test" {
  organization_id = %q
  workspace_id    = %q
  name            = "renamed"
}
`, org, workspace)
	}
	ctx := context.Background()
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			c = testAccClient(t)
			var err error
			ws, err = c.CreateWorkspace(ctx, org, client.NewWorkspace{Slug: slug})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, s := range []string{ws.ID, slug} {
					if err := c.DeleteWorkspace(ctx, org, s); err != nil && !client.IsNotFound(err) {
						t.Error(err)
					}
				}
			})
		},
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{Config: config(slug)},
			{
				PreConfig: func() {
					renamed := slug + "-x"
					if _, err := c.UpdateWorkspace(ctx, org, ws.ID, client.WorkspacePatch{Slug: &renamed}); err != nil {
						t.Fatal(err)
					}
				},
				Config:      config(slug),
				ExpectError: regexp.MustCompile(`Cannot find the configured parents`),
			},
			{
				PreConfig: func() {
					if _, err := c.CreateWorkspace(ctx, org, client.NewWorkspace{Slug: slug}); err != nil {
						t.Fatal(err)
					}
				},
				Config:      config(slug),
				ExpectError: regexp.MustCompile(`Parent changed its slug`),
			},
			{
				Config:           config(slug + "-x"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
	})
}

// TestPlanParents compares parents against a stand-in platform with the
// organizations acme and beta, and the workspace soc in acme.
func TestPlanParents(t *testing.T) {
	const orgA, orgB = "01a1ac68-0000-7000-8000-00000000000a", "01a1ac68-0000-7000-8000-00000000000b"
	const wsA = "01a1ac68-0000-7000-8000-0000000000a1"
	orgs := map[string]string{"acme": orgA, orgA: orgA, "beta": orgB, orgB: orgB}
	mux := http.NewServeMux()
	var server *httptest.Server
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	notFound := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		reply(w, map[string]string{"_tag": "NotFound"})
	}
	mux.HandleFunc("GET "+client.WellKnownPath, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"apiUrl": server.URL + "/api"})
	})
	mux.HandleFunc("GET /api/organizations/{org}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := orgs[r.PathValue("org")]
		if !ok {
			notFound(w)
			return
		}
		reply(w, client.Organization{ID: id})
	})
	mux.HandleFunc("GET /api/organizations/{org}/workspaces/{ws}", func(w http.ResponseWriter, r *http.Request) {
		if orgs[r.PathValue("org")] != orgA || (r.PathValue("ws") != "soc" && r.PathValue("ws") != wsA) {
			notFound(w)
			return
		}
		reply(w, client.Workspace{ID: wsA, OrgID: orgA})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	c, err := client.New(context.Background(), client.Config{URL: server.URL, APIKey: "tnzu_fake"})
	if err != nil {
		t.Fatal(err)
	}

	s := rschema.Schema{Attributes: map[string]rschema.Attribute{
		"organization_id": rschema.StringAttribute{Required: true},
		"workspace_id":    rschema.StringAttribute{Optional: true},
		"version":         rschema.Int64Attribute{Computed: true},
	}}
	objectType := s.Type().TerraformType(context.Background())
	object := func(org, ws any, version any) tftypes.Value {
		return tftypes.NewValue(objectType, map[string]tftypes.Value{
			"organization_id": tftypes.NewValue(tftypes.String, org),
			"workspace_id":    tftypes.NewValue(tftypes.String, ws),
			"version":         tftypes.NewValue(tftypes.Number, version),
		})
	}
	unknown := tftypes.UnknownValue
	tests := []struct {
		name            string
		state, config   tftypes.Value
		plan            tftypes.Value
		requiresReplace bool
		err             string
	}{
		{name: "slug for the ID", state: object(orgA, nil, 1), config: object("acme", nil, nil), plan: object(orgA, nil, 1)},
		{name: "ID for the slug", state: object("acme", "soc", 1), config: object(orgA, wsA, nil), plan: object("acme", "soc", 1)},
		{name: "unchanged slug", state: object("acme", "soc", 1), config: object("acme", "soc", nil), plan: object("acme", "soc", unknown)},
		{name: "another organization", state: object(orgA, nil, 1), config: object("beta", nil, nil), plan: object("beta", nil, unknown), requiresReplace: true},
		{name: "into a workspace", state: object(orgA, nil, 1), config: object("acme", "soc", nil), plan: object(orgA, "soc", unknown), requiresReplace: true},
		{name: "unknown parent", state: object(orgA, nil, 1), config: object(unknown, nil, nil), plan: object(unknown, nil, unknown), requiresReplace: true},
		{name: "unknown slug", state: object(orgA, nil, 1), config: object("gone", nil, nil), err: "Cannot find the configured parents"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			// The framework marks computed attributes unknown when the
			// configuration differs from the state.
			proposed, err := tftypes.Transform(tt.config, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
				if p.Equal(tftypes.NewAttributePath().WithAttributeName("version")) {
					return tftypes.NewValue(tftypes.Number, unknown), nil
				}
				return v, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			req := fwresource.ModifyPlanRequest{
				Config: tfsdk.Config{Schema: s, Raw: tt.config},
				Plan:   tfsdk.Plan{Schema: s, Raw: proposed},
				State:  tfsdk.State{Schema: s, Raw: tt.state},
			}
			resp := fwresource.ModifyPlanResponse{Plan: req.Plan}
			planParents(ctx, c, req, &resp, true)
			if tt.err != "" {
				if !resp.Diagnostics.HasError() || !strings.Contains(fmt.Sprint(resp.Diagnostics), tt.err) {
					t.Fatalf("want error %q, got %v", tt.err, resp.Diagnostics)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if !resp.Plan.Raw.Equal(tt.plan) {
				t.Errorf("plan is %s, want %s", resp.Plan.Raw, tt.plan)
			}
			if got := len(resp.RequiresReplace) > 0; got != tt.requiresReplace {
				t.Errorf("requires replace is %v (%v), want %v", got, resp.RequiresReplace, tt.requiresReplace)
			}
		})
	}
}
