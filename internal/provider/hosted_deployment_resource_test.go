package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

func hostedDeploymentConfig(workspace, name, size string) string {
	return fmt.Sprintf(`
resource "tenzir_hosted_deployment" "test" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = %s
  name            = %q
  size            = %q
  timeouts {
    create = "10m"
  }
}
`, workspace, name, size)
}

// TestAccHostedDeployment runs against a platform that hosts deployments,
// and else checks that the refusal reaches the user.
func TestAccHostedDeployment(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("needs TF_ACC")
	}
	c, err := client.New(context.Background(), client.Config{URL: os.Getenv("TENZIR_PLATFORM_URL"), APIKey: os.Getenv("TENZIR_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	workspace := testAccName("tf-acc-hosted")
	config := func(name, size string) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %q
}
`, workspace) + hostedDeploymentConfig("tenzir_workspace.test.id", name, size)
	}
	if !c.Platform().HostedDeployments {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProviders,
			Steps: []resource.TestStep{{
				Config:      config("refused", "tiny"),
				ExpectError: regexp.MustCompile(`HostedDeploymentRefused`),
			}},
		})
		return
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("before", "tiny"),
				Check:  resource.TestCheckResourceAttrSet("tenzir_hosted_deployment.test", "region"),
			},
			{
				ResourceName:            "tenzir_hosted_deployment.test",
				ImportState:             true,
				ImportStateIdFunc:       importID("tenzir_hosted_deployment.test", "organization_id", "workspace_id", "id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				Config: config("after", "small"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("tenzir_hosted_deployment.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("tenzir_hosted_deployment.test", "size", "small"),
			},
		},
	})
}

// TestAccHostedDeploymentFake drives the resource against a stand-in for a
// platform that hosts deployments, which the dev stack does not.
func TestAccHostedDeploymentFake(t *testing.T) {
	saved := reachablePollInterval
	reachablePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { reachablePollInterval = saved })
	fake := newFakeHostingPlatform(t)
	provider := fmt.Sprintf(`
provider "tenzir" {
  url     = %q
  api_key = "tnzu_fake"
}

data "tenzir_organization" "test" {
  slug = "acme"
}
`, fake.URL)
	config := func(name, size, image string) string {
		return provider + hostedDeploymentConfig(`"soc"`, name, size) + fmt.Sprintf(`
resource "tenzir_hosted_deployment" "pinned" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = "soc"
  name            = "pinned"
  image           = %q
}
`, image)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("before", "tiny", "ghcr.io/tenzir/tenzir:v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_hosted_deployment.test", "region", "europe-west3"),
					resource.TestCheckResourceAttr("tenzir_hosted_deployment.pinned", "size", "medium"),
					func(*terraform.State) error { return fake.check("pinned", "ghcr.io/tenzir/tenzir:v1") },
				),
			},
			{
				ResourceName:            "tenzir_hosted_deployment.test",
				ImportState:             true,
				ImportStateIdFunc:       importID("tenzir_hosted_deployment.test", "organization_id", "workspace_id", "id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "workspace_id"},
			},
			{
				Config: config("after", "small", "ghcr.io/tenzir/tenzir:v1"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("tenzir_hosted_deployment.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("tenzir_hosted_deployment.pinned", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_hosted_deployment.test", "name", "after"),
					resource.TestCheckResourceAttr("tenzir_hosted_deployment.test", "size", "small"),
				),
			},
			{
				Config: config("after", "small", "ghcr.io/tenzir/tenzir:v2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("tenzir_hosted_deployment.pinned", plancheck.ResourceActionReplace)},
				},
				Check: func(*terraform.State) error { return fake.check("pinned", "ghcr.io/tenzir/tenzir:v2") },
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if n := fake.count(); n != 0 {
				return fmt.Errorf("%d deployments remain", n)
			}
			return nil
		},
	})
}

type fakeDeployment struct {
	details client.DeploymentDetails
	image   string
	polls   int
}

type fakeHostingPlatform struct {
	*httptest.Server
	mu          sync.Mutex
	deployments map[string]*fakeDeployment
	next        int
}

// newFakeHostingPlatform serves the calls that tenzir_hosted_deployment
// makes. A deployment connects on the third read of its details.
func newFakeHostingPlatform(t *testing.T) *fakeHostingPlatform {
	f := &fakeHostingPlatform{deployments: map[string]*fakeDeployment{}}
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET "+client.WellKnownPath, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"apiUrl": f.URL + "/api", "hostedDeployments": true})
	})
	mux.HandleFunc("GET /api/organizations/acme", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"id": "org-1", "slug": "acme", "name": "Acme"})
	})
	mux.HandleFunc("GET /api/organizations/{org}/workspaces/{ws}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, client.Workspace{ID: "ws-1", Slug: "soc", OrgID: "org-1"})
	})
	mux.HandleFunc("POST /api/organizations/{org}/workspaces/{ws}/hosted-deployments", func(w http.ResponseWriter, r *http.Request) {
		var body client.NewHostedDeployment
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.next++
		size, region := "medium", "europe-west3"
		if body.Size != nil {
			size = *body.Size
		}
		if body.Region != nil {
			region = *body.Region
		}
		branch := "main"
		if body.Branch != nil {
			branch = *body.Branch
		}
		d := &fakeDeployment{details: client.DeploymentDetails{
			Deployment: client.Deployment{ID: fmt.Sprintf("dep-%d", f.next), WorkspaceID: "ws-1", Name: body.Name},
			Hosting:    "tenzir",
			Size:       &size,
			Region:     &region,
			CreatedAt:  "2026-11-03T00:00:00Z",
			Tracker:    client.Tracker{Branch: branch, AutoDeploy: body.AutoDeploy == nil || *body.AutoDeploy},
		}}
		if body.Image != nil {
			d.image = *body.Image
		}
		f.deployments[d.details.Deployment.ID] = d
		w.WriteHeader(http.StatusCreated)
		reply(w, d.details.Deployment)
	})
	deployment := func(handle func(http.ResponseWriter, *http.Request, *fakeDeployment)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			d, ok := f.deployments[r.PathValue("id")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				reply(w, map[string]string{"_tag": "NotFound"})
				return
			}
			handle(w, r, d)
		}
	}
	mux.HandleFunc("GET /api/organizations/{org}/workspaces/{ws}/deployments/{id}/details", deployment(func(w http.ResponseWriter, r *http.Request, d *fakeDeployment) {
		d.polls++
		d.details.Reachable = d.polls >= 3
		reply(w, d.details)
	}))
	mux.HandleFunc("POST /api/organizations/{org}/workspaces/{ws}/deployments/{id}/update", deployment(func(w http.ResponseWriter, r *http.Request, d *fakeDeployment) {
		var patch client.DeploymentPatch
		_ = json.NewDecoder(r.Body).Decode(&patch)
		if patch.Name != nil {
			d.details.Deployment.Name = *patch.Name
		}
		reply(w, d.details.Deployment)
	}))
	mux.HandleFunc("POST /api/organizations/{org}/workspaces/{ws}/deployments/{id}/change-type", deployment(func(w http.ResponseWriter, r *http.Request, d *fakeDeployment) {
		var body struct{ Size string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.details.Size = &body.Size
		reply(w, body)
	}))
	mux.HandleFunc("DELETE /api/organizations/{org}/workspaces/{ws}/deployments/{id}", deployment(func(w http.ResponseWriter, r *http.Request, d *fakeDeployment) {
		delete(f.deployments, d.details.Deployment.ID)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// check reports whether the one deployment of a name runs an image.
func (f *fakeHostingPlatform) check(name, image string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []string
	for _, d := range f.deployments {
		if d.details.Deployment.Name == name {
			found = append(found, d.image)
		}
	}
	if len(found) != 1 || found[0] != image {
		return fmt.Errorf("deployments named %s run %v, want only %s", name, found, image)
	}
	return nil
}

func (f *fakeHostingPlatform) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deployments)
}
