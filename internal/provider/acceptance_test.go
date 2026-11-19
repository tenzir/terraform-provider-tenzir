package provider

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Acceptance tests run with TF_ACC=1 against the platform at
// TENZIR_PLATFORM_URL, authenticated by TENZIR_API_KEY, and create their
// resources in the organization TENZIR_TEST_ORGANIZATION (an ID or slug).
var testAccProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"tenzir": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	for _, v := range []string{"TENZIR_PLATFORM_URL", "TENZIR_API_KEY", "TENZIR_TEST_ORGANIZATION"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

// testAccOrganization is HCL for the test organization, as
// data.tenzir_organization.test.
func testAccOrganization() string {
	return fmt.Sprintf(`
data "tenzir_organization" "test" {
  slug = %q
}
`, os.Getenv("TENZIR_TEST_ORGANIZATION"))
}

// testAccName returns a unique slug-shaped name.
func testAccName(prefix string) string {
	return fmt.Sprintf("%s-%06d", prefix, rand.Intn(1000000))
}

// importID builds an import ID from attributes of a resource in state.
func importID(name string, attrs ...string) func(*terraform.State) (string, error) {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("%s is not in state", name)
		}
		id := ""
		for i, a := range attrs {
			if i > 0 {
				id += "/"
			}
			id += rs.Primary.Attributes[a]
		}
		return id, nil
	}
}
