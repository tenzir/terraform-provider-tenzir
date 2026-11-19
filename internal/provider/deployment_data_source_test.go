package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestDeploymentUpdateImage(t *testing.T) {
	const org = "01a1ac68-0000-7000-8000-00000000000a"
	const workspace = "01a1ac68-0000-7000-8000-00000000000b"
	for _, tc := range []struct {
		name   string
		update string
		want   types.String
	}{
		{name: "unknown build", want: types.StringNull()},
		{name: "null update", update: `,"update":null`, want: types.StringNull()},
		{name: "current or later build", update: `,"update":{"_tag":"Current"}`, want: types.StringNull()},
		{
			name: "available build", want: types.StringValue("ghcr.io/tenzir/tenzir@sha256:build"),
			update: `,"update":{"_tag":"Available","image":"ghcr.io/tenzir/tenzir@sha256:build","version":"7.0.0"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testPlatformClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/organizations/"+org+"/workspaces/"+workspace+"/deployments/dep/details" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_, _ = fmt.Fprintf(w, `{"deployment":{"id":"dep","workspaceId":%q,"name":"Node","slug":"node","version":"6.0.0"},"reachable":true,"hosting":"customer","createdAt":"2026-11-19T00:00:00Z","tracker":{"branch":"main","autoDeploy":true,"commit":null}%s}`, workspace, tc.update)
			})
			ctx := context.Background()
			d := &deploymentDataSource{client: c}
			var schemaResp datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
			objectType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
			attributes := map[string]tftypes.Value{}
			for name, typ := range objectType.AttributeTypes {
				attributes[name] = tftypes.NewValue(typ, nil)
			}
			attributes["id"] = tftypes.NewValue(tftypes.String, "dep")
			attributes["organization_id"] = tftypes.NewValue(tftypes.String, org)
			attributes["workspace_id"] = tftypes.NewValue(tftypes.String, workspace)
			req := datasource.ReadRequest{Config: tfsdk.Config{
				Schema: schemaResp.Schema, Raw: tftypes.NewValue(objectType, attributes),
			}}
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
			d.Read(ctx, req, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var state deploymentDataModel
			if diags := resp.State.Get(ctx, &state); diags.HasError() {
				t.Fatal(diags)
			}
			if !state.UpgradeImage.Equal(tc.want) {
				t.Errorf("upgrade_image: got %v, want %v", state.UpgradeImage, tc.want)
			}
			if state.Version.ValueString() != "6.0.0" {
				t.Errorf("reported version: %v", state.Version)
			}
		})
	}
}
