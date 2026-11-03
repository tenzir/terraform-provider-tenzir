package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccIngestEndpoint(t *testing.T) {
	name := testAccName("tf-acc")
	config := func(description, readers string) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_ingest_endpoint" "test" {
  organization_id = data.tenzir_organization.test.id
  name            = %q
  description     = %q
  readable_by     = %s
}
`, name, description, readers)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("before", "null"),
				Check:  resource.TestCheckResourceAttrSet("tenzir_ingest_endpoint.test", "token"),
			},
			{
				ResourceName:      "tenzir_ingest_endpoint.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: config("after", "{ deployments = [] }"),
				Check:  resource.TestCheckResourceAttr("tenzir_ingest_endpoint.test", "description", "after"),
			},
		},
	})
}
