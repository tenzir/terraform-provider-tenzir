package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testPlatformClient(t *testing.T, api http.HandlerFunc) *client.Client {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == client.WellKnownPath {
			_ = json.NewEncoder(w).Encode(map[string]string{"apiUrl": server.URL + "/api"})
			return
		}
		api(w, r)
	}))
	t.Cleanup(server.Close)
	c, err := client.New(context.Background(), client.Config{URL: server.URL, APIKey: "tnzu_fake"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAccOrganizationLookups(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: testAccOrganization() + `
data "tenzir_roles" "test" {
  organization_id = data.tenzir_organization.test.id
}

data "tenzir_workspace" "default" {
  organization_id = data.tenzir_organization.test.id
  slug            = "default"
}

data "tenzir_deployment_types" "test" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = data.tenzir_workspace.default.id
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckTypeSetElemNestedAttrs("data.tenzir_roles.test", "roles.*", map[string]string{"id": "org.owner", "builtin": "true"}),
				resource.TestCheckResourceAttrSet("data.tenzir_deployment_types.test", "default_size"),
				resource.TestCheckResourceAttrSet("data.tenzir_deployment_types.test", "types.0.size"),
			),
		}},
	})
}
