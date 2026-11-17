package provider

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccOrganizationAsAdministrator sets an organization up the way Tenzir
// sets up its demo organization. It needs TENZIR_API_KEY to belong to a
// platform administrator, and TENZIR_TEST_MEMBER_EMAIL to name another user.
func TestAccOrganizationAsAdministrator(t *testing.T) {
	email := os.Getenv("TENZIR_TEST_MEMBER_EMAIL")
	if email == "" {
		t.Skip("needs TENZIR_TEST_MEMBER_EMAIL and an administrator's TENZIR_API_KEY")
	}
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", rand.Int63n(1e12))
	slug := testAccName("tf-acc-org")
	config := func(name, roles string) string {
		return fmt.Sprintf(`
resource "tenzir_organization" "test" {
  id   = %q
  slug = %q
  name = %q
}

resource "tenzir_role" "visitor" {
  organization_id = tenzir_organization.test.id
  label           = "Visitor"
  color           = "neutral"
  policy = jsonencode([
    [{ target = { _tag = "Organization", orgId = tenzir_organization.test.id }, relationships = [] }, ["Organization.read"]],
  ])
}

data "tenzir_user" "member" {
  email = %q
}

resource "tenzir_organization_member" "test" {
  organization_id = tenzir_organization.test.slug
  user_id         = data.tenzir_user.member.id
  role_ids        = %s
}
`, id, slug, name, email, roles)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("Before", `[tenzir_role.visitor.id]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization.test", "id", id),
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "role_ids.#", "1"),
				),
			},
			{
				ResourceName:      "tenzir_organization.test",
				ImportState:       true,
				ImportStateId:     slug,
				ImportStateVerify: true,
			},
			{
				ResourceName:            "tenzir_organization_member.test",
				ImportState:             true,
				ImportStateIdFunc:       importID("tenzir_organization_member.test", "organization_id", "user_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"organization_id"},
			},
			{
				Config: config("After", `[tenzir_role.visitor.id, "org.member"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization.test", "name", "After"),
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "role_ids.#", "2"),
				),
			},
			{
				Config: config("After", `[]`),
				Check:  resource.TestCheckResourceAttr("tenzir_organization_member.test", "role_ids.#", "0"),
			},
		},
	})
}
