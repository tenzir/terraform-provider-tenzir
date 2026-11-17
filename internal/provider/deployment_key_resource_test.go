package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDeploymentKey(t *testing.T) {
	workspace := testAccName("tf-acc-keys")
	config := func(rotation int) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %q
}

resource "tenzir_deployment" "test" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = tenzir_workspace.test.id
  name            = "keyed"
}

resource "tenzir_deployment_key" "test" {
  organization_id = tenzir_deployment.test.organization_id
  workspace_id    = tenzir_deployment.test.workspace_id
  deployment_id   = tenzir_deployment.test.id
  triggers        = { rotation = "%d" }
}

data "tenzir_deployment" "test" {
  organization_id = tenzir_deployment_key.test.organization_id
  workspace_id    = tenzir_deployment_key.test.workspace_id
  id              = tenzir_deployment_key.test.deployment_id
}
`, workspace, rotation)
	}
	var first string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config(1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("tenzir_deployment_key.test", "key", func(v string) error {
						first = v
						if v == "" {
							return fmt.Errorf("the key is empty")
						}
						return nil
					}),
					resource.TestCheckResourceAttr("data.tenzir_deployment.test", "hosting", "customer"),
					resource.TestCheckResourceAttr("data.tenzir_deployment.test", "reachable", "false"),
					resource.TestCheckResourceAttrPair("data.tenzir_deployment.test", "workspace_id", "tenzir_workspace.test", "id"),
				),
			},
			{
				Config: config(2),
				Check: resource.TestCheckResourceAttrWith("tenzir_deployment_key.test", "key", func(v string) error {
					if v == first {
						return fmt.Errorf("the key did not change")
					}
					return nil
				}),
			},
		},
	})
}
