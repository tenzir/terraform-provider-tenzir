package provider

import (
	"context"
	"fmt"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAPIKeyName(t *testing.T) {
	ctx := context.Background()
	var resp fwresource.SchemaResponse
	(&apiKeyResource{}).Schema(ctx, fwresource.SchemaRequest{}, &resp)
	name := resp.Schema.Attributes["name"].(schema.StringAttribute)
	if !name.Required {
		t.Fatal("an API key needs a name")
	}
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{name: ""},
		{name: " \t\n"},
		{name: "\u2003\uFEFF\v"},
		{name: "CI key", valid: true},
	} {
		t.Run(fmt.Sprintf("%q", tc.name), func(t *testing.T) {
			var validation validator.StringResponse
			for _, v := range name.Validators {
				v.ValidateString(ctx, validator.StringRequest{ConfigValue: types.StringValue(tc.name)}, &validation)
			}
			if validation.Diagnostics.HasError() == tc.valid {
				t.Errorf("valid = %v, want %v: %v", !validation.Diagnostics.HasError(), tc.valid, validation.Diagnostics)
			}
		})
	}
}

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
				ImportStateIdFunc:       importID("tenzir_api_key.test", "user_id", "id"),
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
