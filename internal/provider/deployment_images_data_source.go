package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tenzir/terraform-provider-tenzir/internal/client"
)

var _ datasource.DataSourceWithConfigure = &deploymentImagesDataSource{}

func NewDeploymentImagesDataSource() datasource.DataSource {
	return &deploymentImagesDataSource{}
}

type deploymentImagesDataSource struct {
	client *client.Client
}

type deploymentImagesModel struct {
	Default types.String           `tfsdk:"default"`
	Images  []deploymentImageModel `tfsdk:"images"`
}

type deploymentImageModel struct {
	Tag       types.String `tfsdk:"tag"`
	Title     types.String `tfsdk:"title"`
	Reference types.String `tfsdk:"reference"`
}

func (d *deploymentImagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment_images"
}

func (d *deploymentImagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The deployment images that match the platform. The platform answers with an " +
			"error when it cannot ask its registry.",
		Attributes: map[string]schema.Attribute{
			"default": schema.StringAttribute{MarkdownDescription: "The image a deployment runs unless told otherwise.", Computed: true},
			"images": schema.ListNestedAttribute{
				MarkdownDescription: "The images since the platform's release, newest first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"tag":       schema.StringAttribute{MarkdownDescription: "The image tag.", Computed: true},
						"title":     schema.StringAttribute{MarkdownDescription: "A human-readable title.", Computed: true},
						"reference": schema.StringAttribute{MarkdownDescription: "The full image reference.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *deploymentImagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics)
}

func (d *deploymentImagesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	images, err := d.client.ListDeploymentImages(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list the deployment images", err.Error())
		return
	}
	state := deploymentImagesModel{Default: types.StringValue(images.Default), Images: []deploymentImageModel{}}
	for _, i := range images.Images {
		state.Images = append(state.Images, deploymentImageModel{
			Tag:       types.StringValue(i.Tag),
			Title:     types.StringValue(i.Title),
			Reference: types.StringValue(i.Reference),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
