package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccWorkspace(t *testing.T) {
	slug := testAccName("tf-acc-ws")
	config := func(slug, name string) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %q
  name            = %q
}

data "tenzir_workspace" "test" {
  organization_id = tenzir_workspace.test.organization_id
  slug            = tenzir_workspace.test.slug
}
`, slug, name)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config(slug, "Before"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.tenzir_workspace.test", "id", "tenzir_workspace.test", "id"),
					resource.TestCheckResourceAttr("data.tenzir_workspace.test", "name", "Before"),
				),
			},
			{
				ResourceName:      "tenzir_workspace.test",
				ImportState:       true,
				ImportStateIdFunc: importID("tenzir_workspace.test", "organization_id", "slug"),
				ImportStateVerify: true,
			},
			{
				Config: config(slug+"-x", "After"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.tenzir_workspace.test", "slug", slug+"-x"),
					resource.TestCheckResourceAttr("data.tenzir_workspace.test", "name", "After"),
				),
			},
		},
	})
}
