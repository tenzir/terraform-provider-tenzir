package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

func TestOrganizationCreation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		plan          types.String
		accountStatus int
		account       string
		createStatus  int
		wantRequests  []string
		wantError     string
	}{
		{
			name: "authenticated user owns a Community organization", plan: types.StringNull(),
			account:      `{"_tag":"User","user":{"id":"caller-id"}}`,
			wantRequests: []string{"GET /api/account", "POST /api/users/caller-id/organizations"},
		},
		{
			name: "administrator provisions without a user lookup", plan: types.StringValue(provisionedPlan),
			wantRequests: []string{"POST /api/organizations"},
		},
		{
			name: "account lookup failure stops creation", plan: types.StringNull(),
			accountStatus: http.StatusUnauthorized, account: `{"_tag":"Unauthorized"}`,
			wantRequests: []string{"GET /api/account"},
			wantError:    "Cannot tell who the provider authenticates as",
		},
		{
			name: "non-user credential cannot own an organization", plan: types.StringNull(),
			account:      `{"_tag":"Organization"}`,
			wantRequests: []string{"GET /api/account"},
			wantError:    "No user to own the organization",
		},
		{
			name: "creation refusal reaches Terraform", plan: types.StringNull(),
			account: `{"_tag":"User","user":{"id":"caller-id"}}`, createStatus: http.StatusForbidden,
			wantRequests: []string{"GET /api/account", "POST /api/users/caller-id/organizations"},
			wantError:    "Cannot create the organization",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var got []string
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == client.WellKnownPath {
					_ = json.NewEncoder(w).Encode(map[string]string{"apiUrl": server.URL + "/api"})
					return
				}
				got = append(got, r.Method+" "+r.URL.Path)
				if r.Method == "GET" && r.URL.Path == "/api/account" {
					if tc.accountStatus != 0 {
						w.WriteHeader(tc.accountStatus)
					}
					_, _ = w.Write([]byte(tc.account))
					return
				}
				if r.Method != "POST" || (r.URL.Path != "/api/organizations" && r.URL.Path != "/api/users/caller-id/organizations") {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if string(body) != `{"slug":"acme","name":"Acme","iconUrl":"https://example.com/icon.png"}` {
					t.Errorf("creation metadata: %s", body)
				}
				if tc.createStatus != 0 {
					w.WriteHeader(tc.createStatus)
					_, _ = w.Write([]byte(`{"_tag":"Forbidden"}`))
					return
				}
				edition, members := "community", 1
				if tc.plan.ValueString() == provisionedPlan {
					edition, members = provisionedPlan, 0
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "org-id", "slug": "acme", "name": "Acme", "iconUrl": "https://example.com/icon.png",
					"edition": edition, "memberCount": members,
				})
			}))
			t.Cleanup(server.Close)
			c, err := client.New(ctx, client.Config{URL: server.URL, APIKey: "tnzu_fake"})
			if err != nil {
				t.Fatal(err)
			}
			r := &organizationResource{client: c}
			var schemaResp fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
			model := organizationResourceModel{
				ID: types.StringUnknown(), Slug: types.StringValue("acme"), Name: types.StringValue("Acme"),
				IconURL: types.StringValue("https://example.com/icon.png"), Plan: tc.plan,
			}
			req := fwresource.CreateRequest{Plan: tfsdk.Plan{Schema: schemaResp.Schema}}
			if diags := req.Plan.Set(ctx, &model); diags.HasError() {
				t.Fatal(diags)
			}
			resp := fwresource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
			r.Create(ctx, req, &resp)
			if !reflect.DeepEqual(got, tc.wantRequests) {
				t.Errorf("requests: got %v, want %v", got, tc.wantRequests)
			}
			if tc.wantError != "" {
				if !resp.Diagnostics.HasError() || !strings.Contains(fmt.Sprint(resp.Diagnostics), tc.wantError) {
					t.Fatalf("want error %q, got %v", tc.wantError, resp.Diagnostics)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var state organizationResourceModel
			if diags := resp.State.Get(ctx, &state); diags.HasError() {
				t.Fatal(diags)
			}
			model.ID = types.StringValue("org-id")
			if state != model {
				t.Errorf("state: got %+v, want %+v", state, model)
			}
		})
	}
}

// TestAccOrganizationAsAdministrator sets an organization up the way Tenzir
// sets up its demo organization, and makes it the platform's demo, replacing
// any other, so it suits only a disposable platform. It needs TENZIR_API_KEY to belong to a
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

data "tenzir_current_user" "test" {}

resource "tenzir_organization_member" "test" {
  organization_id = tenzir_organization.test.id
  user_id         = data.tenzir_current_user.test.id
  role_ids        = ["org.owner"]
}

resource "tenzir_role" "visitor" {
  organization_id = tenzir_organization.test.id
  label           = "Visitor"
  color           = "neutral"
  policy = jsonencode([
    [{ target = { _tag = "Organization", orgId = tenzir_organization.test.id }, relationships = [] }, ["Organization.read"]],
  ])
  depends_on = [tenzir_organization_member.test]
}

resource "tenzir_demo_organization" "test" {
  organization_id = tenzir_organization.test.id
  role_id         = tenzir_role.visitor.id
}
`, slug, name)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config("Before"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tenzir_organization.test", "plan", "provisioned"),
					resource.TestCheckResourceAttr("tenzir_organization_member.test", "role_ids.#", "1"),
					resource.TestCheckResourceAttrPair("tenzir_demo_organization.test", "role_id", "tenzir_role.visitor", "id"),
				),
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
