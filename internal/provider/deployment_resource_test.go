package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

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
