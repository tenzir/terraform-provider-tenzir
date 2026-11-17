package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccOrganizationAsAdministrator sets an organization up the way Tenzir
// sets up its demo organization. It needs TENZIR_API_KEY to belong to a
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
`, slug, name)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("Before"),
				Check:  resource.TestCheckResourceAttr("tenzir_organization.test", "plan", "provisioned"),
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
