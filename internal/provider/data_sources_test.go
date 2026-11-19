package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The dev stack cannot ask a registry, so tenzir_deployment_images is left
// to the hand check against a real platform.
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
