package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

func TestAccSecret(t *testing.T) {
	name := "tf_acc_" + testAccName("secret")[7:]
	config := func(valueVersion int, readers string) string {
		return testAccOrganization() + fmt.Sprintf(`
resource "tenzir_workspace" "test" {
  organization_id = data.tenzir_organization.test.id
  slug            = %[1]q
}

resource "tenzir_secret" "org" {
  organization_id = data.tenzir_organization.test.id
  name            = %[2]q
  value           = "v%[3]d"
  value_version   = %[3]d
  readable_by     = %[4]s
}

resource "tenzir_secret" "workspace" {
  organization_id = data.tenzir_organization.test.id
  workspace_id    = tenzir_workspace.test.id
  name            = %[2]q
  value           = "w"
  readable_by     = { deployments = [] }
}
`, testAccName("tf-acc-secrets"), name, valueVersion, readers)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config(1, "null"),
				Check:  resource.TestCheckResourceAttr("tenzir_secret.org", "version", "1"),
			},
			{
				ResourceName:            "tenzir_secret.workspace",
				ImportState:             true,
				ImportStateIdFunc:       importID("tenzir_secret.workspace", "organization_id", "workspace_id", "name"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"value"},
			},
			{
				Config: config(2, "{ workspaces = [tenzir_workspace.test.id] }"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_secret.org", "version", "2"),
					resource.TestCheckResourceAttrPair("tenzir_secret.org", "readable_by.workspaces.0", "tenzir_workspace.test", "id"),
				),
			},
			{
				PreConfig: func() { overwriteSecret(t, name) },
				Config:    config(2, "{ workspaces = [tenzir_workspace.test.id] }"),
				Check:     resource.TestCheckResourceAttr("tenzir_secret.org", "version", "4"),
			},
		},
	})
}

// overwriteSecret changes the value outside Terraform.
func overwriteSecret(t *testing.T, name string) {
	ctx := context.Background()
	c, err := client.New(ctx, client.Config{URL: os.Getenv("TENZIR_PLATFORM_URL"), APIKey: os.Getenv("TENZIR_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	value := "changed elsewhere"
	scope := client.SecretScope{Organization: os.Getenv("TENZIR_TEST_ORGANIZATION")}
	if _, err := c.SetSecret(ctx, scope, name, client.SecretWrite{Value: &value}); err != nil {
		t.Fatal(err)
	}
}
