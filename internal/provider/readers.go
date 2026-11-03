package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

// readableByModel restricts which deployments may read a secret or an ingest
// endpoint. A null readable_by lets every deployment read.
type readableByModel struct {
	Workspaces  types.Set `tfsdk:"workspaces"`
	Deployments types.Set `tfsdk:"deployments"`
}

var readableByAttrTypes = map[string]attr.Type{
	"workspaces":  types.SetType{ElemType: types.StringType},
	"deployments": types.SetType{ElemType: types.StringType},
}

func readableBySchema(what string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Restricts which deployments may read the " + what + ". Without it, every " +
			"deployment that the owner reaches may read; with it, only those listed, and none when both lists are empty.",
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"workspaces": schema.SetAttribute{
				MarkdownDescription: "IDs of workspaces whose deployments, including future ones, may read.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"deployments": schema.SetAttribute{
				MarkdownDescription: "IDs of deployments that may read.",
				ElementType:         types.StringType,
				Optional:            true,
			},
		},
	}
}

var defaultObjectAsOptions = basetypes.ObjectAsOptions{}

// readableByToAPI maps a null object to everyone.
func readableByToAPI(ctx context.Context, v types.Object) (client.ReadableBy, diag.Diagnostics) {
	if v.IsNull() {
		return client.Everyone, nil
	}
	var m readableByModel
	diags := v.As(ctx, &m, defaultObjectAsOptions)
	var out client.ReadableBy
	diags.Append(m.Workspaces.ElementsAs(ctx, &out.Workspaces, true)...)
	diags.Append(m.Deployments.ElementsAs(ctx, &out.Deployments, true)...)
	return out, diags
}

// readableByFromAPI keeps a list null where the platform reports it empty and
// the prior value had it null, since both mean no reader.
func readableByFromAPI(ctx context.Context, r client.ReadableBy, prior types.Object) (types.Object, diag.Diagnostics) {
	if r.Everyone {
		return types.ObjectNull(readableByAttrTypes), nil
	}
	var p readableByModel
	var diags diag.Diagnostics
	if !prior.IsNull() && !prior.IsUnknown() {
		diags = prior.As(ctx, &p, defaultObjectAsOptions)
	} else {
		p = readableByModel{Workspaces: types.SetNull(types.StringType), Deployments: types.SetNull(types.StringType)}
	}
	list := func(ids []string, prior types.Set) types.Set {
		if len(ids) == 0 && prior.IsNull() {
			return types.SetNull(types.StringType)
		}
		s, d := types.SetValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		return s
	}
	obj, d := types.ObjectValueFrom(ctx, readableByAttrTypes, readableByModel{
		Workspaces:  list(r.Workspaces, p.Workspaces),
		Deployments: list(r.Deployments, p.Deployments),
	})
	diags.Append(d...)
	return obj, diags
}
