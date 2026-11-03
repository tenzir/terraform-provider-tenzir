package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPIKey(t *testing.T) {
	config := func(name, policy string) string {
		return fmt.Sprintf(`
resource "tenzir_api_key" "test" {
  name   = %q
  policy = jsonencode(%s)
}
`, name, policy)
	}
	introspect := `[[{ target = { _tag = "User" }, relationships = ["self"] }, ["User.listApiKeys"]]]`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("tf-acc-key", "[]"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_api_key.test", "key"),
					resource.TestCheckResourceAttrSet("tenzir_api_key.test", "user_id"),
				),
			},
			{
				ResourceName:            "tenzir_api_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"key"},
			},
			{
				Config: config("tf-acc-key-renamed", introspect),
				Check:  resource.TestCheckResourceAttr("tenzir_api_key.test", "name", "tf-acc-key-renamed"),
			},
		},
	})
}
