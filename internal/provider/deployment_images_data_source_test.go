package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDeploymentImagesDefault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		want      types.String
		wantCount int
	}{
		{name: "registry unavailable", body: `{"release":false,"images":[]}`, want: types.StringNull()},
		{name: "no matching image", body: `{"release":true,"default":null,"images":[]}`, want: types.StringNull()},
		{
			name: "matching image", want: types.StringValue("ghcr.io/tenzir/tenzir:7.0.0"), wantCount: 1,
			body: `{"release":true,"default":"ghcr.io/tenzir/tenzir:7.0.0","images":[{"tag":"7.0.0","title":"Tenzir 7.0.0","reference":"ghcr.io/tenzir/tenzir:7.0.0"}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testPlatformClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/deployment-images" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			})
			ctx := context.Background()
			d := &deploymentImagesDataSource{client: c}
			var schemaResp datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
			d.Read(ctx, datasource.ReadRequest{}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var state deploymentImagesModel
			if diags := resp.State.Get(ctx, &state); diags.HasError() {
				t.Fatal(diags)
			}
			if !state.Default.Equal(tc.want) || len(state.Images) != tc.wantCount {
				t.Errorf("images: got %+v, want default %v and %d images", state, tc.want, tc.wantCount)
			}
		})
	}
}
