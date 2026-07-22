package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccProtoV6ProviderFactories instantiates the provider in-process for
// acceptance tests, so tests exercise the same code paths as a real binary.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"tenzir": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	for _, v := range []string{"TENZIR_PLATFORM_ENDPOINT", "TENZIR_PLATFORM_ID_TOKEN"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

// Acceptance tests run against a real Tenzir Platform instance and are only
// executed when TF_ACC is set (e.g. via `make testacc`). The authenticated
// test user must not already belong to an organization.
//
// This test exercises the full resource chain: an organization, an
// org-owned workspace, and a node registration inside that workspace.
func TestAccStack(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_node" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-node"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization.test", "name", "tf-acc-org"),
					resource.TestCheckResourceAttrSet("tenzir_organization.test", "id"),
					resource.TestCheckResourceAttr("tenzir_workspace.test", "name", "tf-acc-workspace"),
					resource.TestCheckResourceAttr("tenzir_workspace.test", "owner_namespace", "organization"),
					resource.TestCheckResourceAttrPair(
						"tenzir_workspace.test", "owner_id",
						"tenzir_organization.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_node.test", "name", "tf-acc-node"),
					resource.TestCheckResourceAttrSet("tenzir_node.test", "id"),
					resource.TestCheckResourceAttrSet("tenzir_node.test", "token"),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_workspace.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "tenzir_node.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_node.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_node.test not found in state")
					}
					return rs.Primary.Attributes["workspace_id"] + "/" + rs.Primary.ID, nil
				},
			},
			// Update and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org-renamed"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace-renamed"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_node" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-node-renamed"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization.test", "name", "tf-acc-org-renamed"),
					resource.TestCheckResourceAttr("tenzir_workspace.test", "name", "tf-acc-workspace-renamed"),
					resource.TestCheckResourceAttr("tenzir_node.test", "name", "tf-acc-node-renamed"),
				),
			},
			// Delete happens automatically at the end, in reverse
			// dependency order.
		},
	})
}

// This test exercises the tenzir_library_source resource: create, import,
// and the in-place update of its settings.
func TestAccLibrarySource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_library_source" "test" {
  organization_id = tenzir_organization.test.id
  name            = "tf-acc-source"
  github_url      = "https://github.com/tenzir/library"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_library_source.test", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_library_source.test", "organization_id",
						"tenzir_organization.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_library_source.test", "name", "tf-acc-source"),
					resource.TestCheckResourceAttr("tenzir_library_source.test",
						"github_url", "https://github.com/tenzir/library"),
					resource.TestCheckResourceAttr("tenzir_library_source.test", "branch_or_ref", "main"),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_library_source.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_library_source.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_library_source.test not found in state")
					}
					return rs.Primary.Attributes["organization_id"] + "/" + rs.Primary.ID, nil
				},
			},
			// Changing the settings updates the source in place.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_library_source" "test" {
  organization_id = tenzir_organization.test.id
  name            = "tf-acc-source-renamed"
  github_url      = "https://github.com/tenzir/library"
  branch_or_ref   = "stable"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_library_source.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_library_source.test", "name", "tf-acc-source-renamed"),
					resource.TestCheckResourceAttr("tenzir_library_source.test", "branch_or_ref", "stable"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_organization_member resource: create,
// import, and the in-place role update. The member's user id is a synthetic
// one that must not already belong to an organization; the platform does not
// require the user to exist.
func TestAccOrganizationMember(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_organization_member" "test" {
  organization_id = tenzir_organization.test.id
  user_id         = "tf-acc-member-user"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"tenzir_organization_member.test", "organization_id",
						"tenzir_organization.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "user_id", "tf-acc-member-user"),
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "role", "member"),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_organization_member.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_organization_member.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_organization_member.test not found in state")
					}
					return rs.Primary.Attributes["organization_id"] + "/" +
						rs.Primary.Attributes["user_id"], nil
				},
			},
			// Changing the role updates the membership in place.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_organization_member" "test" {
  organization_id = tenzir_organization.test.id
  user_id         = "tf-acc-member-user"
  role            = "admin"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_organization_member.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "role", "admin"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_service_token resource: create, import,
// and the forced replacement on attribute changes (the platform has no
// update operation for service tokens).
func TestAccServiceToken(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_service_token" "test" {
  organization_id = tenzir_organization.test.id
  label           = "tf-acc-token"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_service_token.test", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_service_token.test", "organization_id",
						"tenzir_organization.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_service_token.test", "label", "tf-acc-token"),
					resource.TestCheckResourceAttr("tenzir_service_token.test", "role", "member"),
					resource.TestCheckResourceAttrSet("tenzir_service_token.test", "token"),
				),
			},
			// ImportState. The plaintext secret is only revealed at creation
			// time, so the imported state cannot contain the `token`
			// attribute.
			{
				ResourceName:            "tenzir_service_token.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"token"},
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_service_token.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_service_token.test not found in state")
					}
					return rs.Primary.Attributes["organization_id"] + "/" + rs.Primary.ID, nil
				},
			},
			// Changing an attribute forces a replacement.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_service_token" "test" {
  organization_id = tenzir_organization.test.id
  label           = "tf-acc-token"
  role            = "admin"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_service_token.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_service_token.test", "role", "admin"),
					resource.TestCheckResourceAttrSet("tenzir_service_token.test", "token"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_secret resource: create, the in-place
// value update, the forced replacement on rename (the platform has no
// rename operation for secrets), the in-place switch from `value` to the
// write-only `value_wo`, and the in-place update triggered by a
// `value_wo_version` bump. There is no import step: the platform never
// returns secret values, so secrets cannot be imported. The write-only
// steps require Terraform >= 1.11.
func TestAccSecret(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-secret"
  value        = "initial-value"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_secret.test", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_secret.test", "workspace_id",
						"tenzir_workspace.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_secret.test", "name", "tf-acc-secret"),
					resource.TestCheckResourceAttr("tenzir_secret.test", "value", "initial-value"),
					resource.TestCheckNoResourceAttr("tenzir_secret.test", "store_id"),
				),
			},
			// Changing the value updates the secret in place.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-secret"
  value        = "updated-value"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_secret.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_secret.test", "value", "updated-value"),
				),
			},
			// Changing the name forces a replacement.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-secret-renamed"
  value        = "updated-value"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_secret.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_secret.test", "name", "tf-acc-secret-renamed"),
				),
			},
			// Switching from `value` to the write-only `value_wo` updates
			// the secret in place; neither value ends up in the state.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret" "test" {
  workspace_id     = tenzir_workspace.test.id
  name             = "tf-acc-secret-renamed"
  value_wo         = "write-only-value"
  value_wo_version = "1"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_secret.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("tenzir_secret.test", "value"),
					resource.TestCheckNoResourceAttr("tenzir_secret.test", "value_wo"),
					resource.TestCheckResourceAttr("tenzir_secret.test", "value_wo_version", "1"),
				),
			},
			// Bumping `value_wo_version` updates the secret in place with
			// the new write-only value.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret" "test" {
  workspace_id     = tenzir_workspace.test.id
  name             = "tf-acc-secret-renamed"
  value_wo         = "write-only-value-2"
  value_wo_version = "2"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_secret.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("tenzir_secret.test", "value_wo"),
					resource.TestCheckResourceAttr("tenzir_secret.test", "value_wo_version", "2"),
				),
			},
			// Renaming still forces a replacement, exercising Create with a
			// write-only value.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret" "test" {
  workspace_id     = tenzir_workspace.test.id
  name             = "tf-acc-secret-wo"
  value_wo         = "write-only-value-2"
  value_wo_version = "2"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_secret.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_secret.test", "name", "tf-acc-secret-wo"),
					resource.TestCheckNoResourceAttr("tenzir_secret.test", "value"),
					resource.TestCheckNoResourceAttr("tenzir_secret.test", "value_wo"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_secret_store resource: create (one AWS and
// one Vault store), import, and the forced replacement on configuration
// changes (the platform has no update operation for secret stores).
//
// Neither store is marked as the workspace default: the platform can only
// move the default to another store, never unset it, so a store with
// `default = true` could not be deleted during cleanup.
func TestAccSecretStore(t *testing.T) {
	config := `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_secret_store" "aws" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-aws-store"
  is_writable  = true
  aws = {
    region           = "%s"
    assumed_role_arn = "arn:aws:iam::123456789012:role/tf-acc"
  }
}

resource "tenzir_secret_store" "vault" {
  workspace_id = tenzir_workspace.test.id
  vault = {
    address     = "https://vault.example.com:8200"
    mount       = "secret"
    auth_method = "token"
    token       = "tf-acc-vault-token"
  }
}
`
	storeImportStateIdFunc := func(name string) resource.ImportStateIdFunc {
		return func(s *terraform.State) (string, error) {
			rs, ok := s.RootModule().Resources[name]
			if !ok {
				return "", fmt.Errorf("%s not found in state", name)
			}
			return rs.Primary.Attributes["workspace_id"] + "/" + rs.Primary.ID, nil
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: fmt.Sprintf(config, "eu-west-1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_secret_store.aws", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_secret_store.aws", "workspace_id",
						"tenzir_workspace.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_secret_store.aws", "name", "tf-acc-aws-store"),
					resource.TestCheckResourceAttr("tenzir_secret_store.aws", "is_writable", "true"),
					resource.TestCheckResourceAttr("tenzir_secret_store.aws", "default", "false"),
					resource.TestCheckResourceAttr("tenzir_secret_store.aws", "aws.region", "eu-west-1"),
					resource.TestCheckResourceAttrSet("tenzir_secret_store.vault", "id"),
					// The platform derives a default name from the store type.
					resource.TestCheckResourceAttr("tenzir_secret_store.vault", "name", "HashiCorp Vault"),
					// Vault stores are always read-only.
					resource.TestCheckResourceAttr("tenzir_secret_store.vault", "is_writable", "false"),
					resource.TestCheckResourceAttr("tenzir_secret_store.vault", "default", "false"),
					resource.TestCheckResourceAttr("tenzir_secret_store.vault",
						"vault.address", "https://vault.example.com:8200"),
					resource.TestCheckResourceAttr("tenzir_secret_store.vault", "vault.auth_method", "token"),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_secret_store.aws",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: storeImportStateIdFunc("tenzir_secret_store.aws"),
			},
			// The Vault token is never returned by the platform, so the
			// imported state cannot contain it.
			{
				ResourceName:            "tenzir_secret_store.vault",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"vault.token"},
				ImportStateIdFunc:       storeImportStateIdFunc("tenzir_secret_store.vault"),
			},
			// Changing the configuration forces a replacement.
			{
				Config: fmt.Sprintf(config, "us-east-1"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_secret_store.aws", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_secret_store.aws", "aws.region", "us-east-1"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_nodes and tenzir_organization_members data
// sources against resources created in the same configuration.
func TestAccDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_node" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-node"
}

data "tenzir_nodes" "test" {
  workspace_id = tenzir_workspace.test.id
  depends_on   = [tenzir_node.test]
}

data "tenzir_organization_members" "test" {
  organization_id = tenzir_organization.test.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.tenzir_nodes.test", "nodes.#", "1"),
					resource.TestCheckResourceAttrPair(
						"data.tenzir_nodes.test", "nodes.0.id",
						"tenzir_node.test", "id",
					),
					resource.TestCheckResourceAttr("data.tenzir_nodes.test", "nodes.0.name", "tf-acc-node"),
					resource.TestCheckResourceAttr("data.tenzir_nodes.test", "nodes.0.demo", "false"),
					resource.TestCheckResourceAttr("data.tenzir_nodes.test", "nodes.0.ephemeral", "false"),
					// The organization contains exactly its creator.
					resource.TestCheckResourceAttr("data.tenzir_organization_members.test", "members.#", "1"),
					resource.TestCheckResourceAttrSet(
						"data.tenzir_organization_members.test", "members.0.user_id"),
					resource.TestCheckResourceAttr(
						"data.tenzir_organization_members.test", "members.0.role", "admin"),
				),
			},
		},
	})
}

// This test exercises the tenzir_alert resource: create, import, and the
// forced replacement on attribute changes (the platform has no update
// operation for alerts).
func TestAccAlert(t *testing.T) {
	config := `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_node" "test" {
  workspace_id = tenzir_workspace.test.id
  name         = "tf-acc-node"
}

resource "tenzir_alert" "test" {
  workspace_id = tenzir_workspace.test.id
  node_id      = tenzir_node.test.id
  duration     = %d
  webhook_url  = "https://hooks.example.com/tf-acc"
  webhook_body = jsonencode({
    text = "node went offline"
  })
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: fmt.Sprintf(config, 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_alert.test", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_alert.test", "workspace_id",
						"tenzir_workspace.test", "id",
					),
					resource.TestCheckResourceAttrPair(
						"tenzir_alert.test", "node_id",
						"tenzir_node.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_alert.test", "duration", "300"),
					resource.TestCheckResourceAttr("tenzir_alert.test",
						"webhook_url", "https://hooks.example.com/tf-acc"),
					resource.TestCheckResourceAttr("tenzir_alert.test",
						"webhook_body", `{"text":"node went offline"}`),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_alert.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_alert.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_alert.test not found in state")
					}
					return rs.Primary.Attributes["workspace_id"] + "/" + rs.Primary.ID, nil
				},
			},
			// Changing an attribute forces a replacement.
			{
				Config: fmt.Sprintf(config, 600),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_alert.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_alert.test", "duration", "600"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_workspace_auth_rule resource: create,
// import, and the forced replacement on attribute changes (the platform has
// no update operation for auth rules).
func TestAccWorkspaceAuthRule(t *testing.T) {
	config := `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_workspace" "test" {
  name       = "tf-acc-workspace"
  depends_on = [tenzir_organization.test]
}

resource "tenzir_workspace_auth_rule" "test" {
  workspace_id = tenzir_workspace.test.id
  target       = "access"
  rule = jsonencode({
    auth_fn = "auth_user"
    user_id = "%s"
  })
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: fmt.Sprintf(config, "tf-acc-rule-user"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_workspace_auth_rule.test", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_workspace_auth_rule.test", "workspace_id",
						"tenzir_workspace.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_workspace_auth_rule.test", "target", "access"),
					resource.TestCheckResourceAttr("tenzir_workspace_auth_rule.test",
						"rule", `{"auth_fn":"auth_user","user_id":"tf-acc-rule-user"}`),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_workspace_auth_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_workspace_auth_rule.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_workspace_auth_rule.test not found in state")
					}
					return rs.Primary.Attributes["workspace_id"] + "/" +
						rs.Primary.Attributes["target"] + "/" + rs.Primary.ID, nil
				},
			},
			// Changing the rule forces a replacement.
			{
				Config: fmt.Sprintf(config, "tf-acc-rule-user-changed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_workspace_auth_rule.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_workspace_auth_rule.test",
						"rule", `{"auth_fn":"auth_user","user_id":"tf-acc-rule-user-changed"}`),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}

// This test exercises the tenzir_federated_credential resource: create,
// import, and the forced replacement on attribute changes (the platform has
// no update operation for federated credentials).
func TestAccFederatedCredential(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_federated_credential" "test" {
  organization_id = tenzir_organization.test.id
  label           = "tf-acc-credential"
  issuer          = "https://token.actions.githubusercontent.com"
  audience        = "tenzir-platform"
  claim_conditions = {
    repository = "tenzir/example"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tenzir_federated_credential.test", "id"),
					resource.TestCheckResourceAttrPair(
						"tenzir_federated_credential.test", "organization_id",
						"tenzir_organization.test", "id",
					),
					resource.TestCheckResourceAttr("tenzir_federated_credential.test", "label", "tf-acc-credential"),
					resource.TestCheckResourceAttr("tenzir_federated_credential.test",
						"issuer", "https://token.actions.githubusercontent.com"),
					resource.TestCheckResourceAttr("tenzir_federated_credential.test", "audience", "tenzir-platform"),
					resource.TestCheckResourceAttr("tenzir_federated_credential.test",
						"claim_conditions.repository", "tenzir/example"),
					resource.TestCheckResourceAttr("tenzir_federated_credential.test", "role", "member"),
				),
			},
			// ImportState.
			{
				ResourceName:      "tenzir_federated_credential.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["tenzir_federated_credential.test"]
					if !ok {
						return "", fmt.Errorf("tenzir_federated_credential.test not found in state")
					}
					return rs.Primary.Attributes["organization_id"] + "/" + rs.Primary.ID, nil
				},
			},
			// Changing an attribute forces a replacement.
			{
				Config: `
resource "tenzir_organization" "test" {
  name = "tf-acc-org"
}

resource "tenzir_federated_credential" "test" {
  organization_id = tenzir_organization.test.id
  label           = "tf-acc-credential"
  issuer          = "https://token.actions.githubusercontent.com"
  audience        = "tenzir-platform"
  claim_conditions = {
    repository = "tenzir/example"
  }
  role = "admin"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(
							"tenzir_federated_credential.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_federated_credential.test", "role", "admin"),
				),
			},
			// Delete happens automatically at the end.
		},
	})
}
