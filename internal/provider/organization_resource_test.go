package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccOrganizationAsAdministrator sets an organization up the way Tenzir
// sets up its demo organization, and makes it the platform's demo, replacing
// any other, so it suits only a disposable platform. It needs TENZIR_API_KEY to belong to a
// platform administrator, which TENZIR_TEST_ADMINISTRATOR confirms.
func TestAccOrganizationAsAdministrator(t *testing.T) {
	if os.Getenv("TENZIR_TEST_ADMINISTRATOR") == "" {
		t.Skip("needs TENZIR_TEST_ADMINISTRATOR and an administrator's TENZIR_API_KEY")
	}
	slug := testAccName("tf-acc-org")
	config := func(name string) string {
		return fmt.Sprintf(`
resource "tenzir_organization" "test" {
  slug = %q
  name = %q
  plan = "provisioned"
}

data "tenzir_current_user" "test" {}

resource "tenzir_organization_member" "test" {
  organization_id = tenzir_organization.test.id
  user_id         = data.tenzir_current_user.test.id
  role_ids        = ["org.owner"]
}

resource "tenzir_role" "visitor" {
  organization_id = tenzir_organization.test.id
  label           = "Visitor"
  color           = "neutral"
  policy = jsonencode([
    [{ target = { _tag = "Organization", orgId = tenzir_organization.test.id }, relationships = [] }, ["Organization.read"]],
  ])
  depends_on = [tenzir_organization_member.test]
}

resource "tenzir_demo_organization" "test" {
  organization_id = tenzir_organization.test.id
  role_id         = tenzir_role.visitor.id
}
`, slug, name)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("Before"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization.test", "plan", "provisioned"),
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "role_ids.#", "1"),
					resource.TestCheckResourceAttrPair("tenzir_demo_organization.test", "role_id", "tenzir_role.visitor", "id"),
				),
			},
			{
				ResourceName:      "tenzir_organization.test",
				ImportState:       true,
				ImportStateId:     slug,
				ImportStateVerify: true,
			},
			{
				Config: config("After"),
				Check:  resource.TestCheckResourceAttr("tenzir_organization.test", "name", "After"),
			},
		},
	})
}
