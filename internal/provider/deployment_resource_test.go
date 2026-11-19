package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

func TestDeploymentRenamePreservesSuccessfulChanges(t *testing.T) {
	var requests []string
	c := testPlatformClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, r.Method+" "+r.URL.Path+" "+string(body))
		switch r.URL.Path {
		case "/api/organizations/org/workspaces/ws/deployments/dep/rename":
			_, _ = w.Write([]byte(`{"id":"dep","workspaceId":"ws","name":"After","slug":"before"}`))
		case "/api/organizations/org/workspaces/ws/deployments/dep/tracker/update":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"_tag":"Unauthorized"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	state := deploymentModel{
		ID: types.StringValue("dep"), Name: types.StringValue("Before"),
		Branch: types.StringValue("main"), AutoDeploy: types.BoolValue(false),
	}
	plan := state
	plan.Name = types.StringValue("After")
	plan.AutoDeploy = types.BoolValue(true)
	err := plan.update(context.Background(), c, client.DeploymentRef{Organization: "org", Workspace: "ws", ID: "dep"}, &state)
	var apiErr *client.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("want a tracker refusal, got %v", err)
	}
	want := []string{
		`POST /api/organizations/org/workspaces/ws/deployments/dep/rename {"name":"After"}`,
		`POST /api/organizations/org/workspaces/ws/deployments/dep/tracker/update {"branch":"main","autoDeploy":true}`,
	}
	if !reflect.DeepEqual(requests, want) {
		t.Errorf("requests: got %v, want %v", requests, want)
	}
	if !state.Name.Equal(plan.Name) || state.AutoDeploy.ValueBool() {
		t.Errorf("state must keep the successful rename and the unchanged tracker: %+v", state)
	}
}

func TestAccDeployment(t *testing.T) {
	workspace := testAccName("tf-acc-deployments")
	config := func(name string, autoDeploy bool) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %q
}

resource "tenzir_deployment" "test" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = tenzir_workspace.test.slug
  name            = %q
  auto_deploy     = %t
}
`, workspace, name, autoDeploy)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("before", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_deployment.test", "branch", "main"),
					resource.TestCheckResourceAttrSet("tenzir_deployment.test", "created_at"),
				),
			},
			{
				ResourceName:            "tenzir_deployment.test",
				ImportState:             true,
				ImportStateIdFunc:       importID("tenzir_deployment.test", "organization_id", "workspace_id", "id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"workspace_id"},
			},
			{
				Config: config("after", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("tenzir_deployment.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_deployment.test", "name", "after"),
					resource.TestCheckResourceAttr("tenzir_deployment.test", "auto_deploy", "true"),
				),
			},
		},
	})
}
