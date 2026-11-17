package provider

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestOrganizationValidateConfig(t *testing.T) {
	ctx := context.Background()
	r := &organizationResource{}
	var schema fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schema)
	typ := schema.Schema.Type().TerraformType(ctx)
	for _, tc := range []struct {
		name     string
		id, plan any
		fails    bool
	}{
		{"chooses no ID", nil, nil, false},
		{"provisions under a chosen ID", "01a1a205-9e4b-76ab-a7ba-cb8a5c23e632", "provisioned", false},
		{"provisions under a generated ID", nil, "provisioned", false},
		{"chooses an ID without provisioning", "01a1a205-9e4b-76ab-a7ba-cb8a5c23e632", nil, true},
		{"chooses a computed ID without provisioning", tftypes.UnknownValue, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := tfsdk.Config{Schema: schema.Schema, Raw: tftypes.NewValue(typ, map[string]tftypes.Value{
				"id":       tftypes.NewValue(tftypes.String, tc.id),
				"slug":     tftypes.NewValue(tftypes.String, "demo"),
				"name":     tftypes.NewValue(tftypes.String, nil),
				"icon_url": tftypes.NewValue(tftypes.String, nil),
				"plan":     tftypes.NewValue(tftypes.String, tc.plan),
			})}
			var resp fwresource.ValidateConfigResponse
			r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{Config: config}, &resp)
			if resp.Diagnostics.HasError() != tc.fails {
				t.Errorf("got %v", resp.Diagnostics)
			}
		})
	}
}

// TestAccOrganizationAsAdministrator sets an organization up the way Tenzir
// sets up its demo organization. It needs TENZIR_API_KEY to belong to a
// platform administrator, which TENZIR_TEST_ADMINISTRATOR confirms.
func TestAccOrganizationAsAdministrator(t *testing.T) {
	if os.Getenv("TENZIR_TEST_ADMINISTRATOR") == "" {
		t.Skip("needs TENZIR_TEST_ADMINISTRATOR and an administrator's TENZIR_API_KEY")
	}
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", rand.Int63n(1e12))
	slug := testAccName("tf-acc-org")
	config := func(name string) string {
		return fmt.Sprintf(`
resource "tenzir_organization" "test" {
  id   = %q
  slug = %q
  name = %q
  plan = "provisioned"
}
`, id, slug, name)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("Before"),
				Check:  resource.TestCheckResourceAttr("tenzir_organization.test", "id", id),
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
