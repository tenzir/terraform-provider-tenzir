package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccRole(t *testing.T) {
	config := func(label, policy string) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_role" "test" {
  organization_id = data.tenzir_organization.test.id
  label           = %q
  color           = "green"
  policy          = jsonencode(%s)
}
`, label, policy)
	}
	// The platform drops the second grant, which the first one covers, and
	// reorders the rest.
	redundant := `[
    [{ target = { _tag = "Workspace" }, relationships = [] }, ["Workspace.read", "Workspace.read"]],
    [{ target = { _tag = "Workspace", orgId = data.tenzir_organization.test.id }, relationships = [] }, ["Workspace.read", "Workspace.update"]],
  ]`
	narrower := `[[{ target = { _tag = "Workspace", orgId = data.tenzir_organization.test.id }, relationships = [] }, ["Workspace.read"]]]`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{Config: config("tf-acc-role", redundant)},
			{
				ResourceName:      "tenzir_role.test",
				ImportState:       true,
				ImportStateIdFunc: importID("tenzir_role.test", "organization_id", "id"),
				ImportStateVerify: true,
				// The import holds the normalized policy, which equals the
				// configured one only semantically.
				ImportStateVerifyIgnore: []string{"policy"},
			},
			{
				Config: config("tf-acc-role-renamed", narrower),
				Check:  resource.TestCheckResourceAttr("tenzir_role.test", "label", "tf-acc-role-renamed"),
			},
		},
	})
}

func TestAccRoleAssignment(t *testing.T) {
	config := func(role string) string {
		return testAccOrganization() + fmt.Sprintf(`
data "tenzir_people" "test" {
  organization_id = data.tenzir_organization.test.id
}

resource "tenzir_role" "test" {
  organization_id = data.tenzir_organization.test.id
  label           = "tf-acc-assigned"
  policy          = jsonencode([])
}

resource "tenzir_role_assignment" "test" {
  organization_id = data.tenzir_organization.test.id
  user_id         = data.tenzir_people.test.people[0].user_id
  role_id         = %s
}
`, role)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{Config: config("tenzir_role.test.id")},
			{
				ResourceName:      "tenzir_role_assignment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: config(`"org.viewer"`),
				Check:  resource.TestCheckResourceAttr("tenzir_role_assignment.test", "role_id", "org.viewer"),
			},
		},
	})
}
