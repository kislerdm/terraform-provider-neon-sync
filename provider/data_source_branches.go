package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonBranchesDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonBranchesDataSource)(nil)

type neonBranchesDataSource struct {
	client *neon.Client
}

type neonBranchesDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Branches  types.List   `tfsdk:"branches"`
}

type branchModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ParentID    types.String `tfsdk:"parent_id"`
	LogicalSize types.Int64  `tfsdk:"logical_size"`
	Primary     types.Bool   `tfsdk:"primary"`
}

func NewBranchesDataSource() datasource.DataSource {
	return &neonBranchesDataSource{}
}

func (d *neonBranchesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_branches"
}

func (d *neonBranchesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetch project branches from one API response. Additional pages are not fetched.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Branch-list scope in the form `<project_id>/branches`.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"branches": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Project branches in API response order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Branch ID.",
						},
						"name": schema.StringAttribute{
							Optional:    true,
							Computed:    true,
							Description: "Branch name.",
						},
						"parent_id": schema.StringAttribute{
							Optional:    true,
							Computed:    true,
							Description: "Parent branch ID. Empty when the API omits it.",
						},
						"logical_size": schema.Int64Attribute{
							Computed:    true,
							Description: "Branch logical size in bytes. Zero when the API omits it.",
						},
						"primary": schema.BoolAttribute{
							Computed:    true,
							Description: "Legacy primary branch flag. False when the API omits it.",
						},
					},
				},
			},
		},
	}
}

func (d *neonBranchesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if adapter == nil || adapter.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}
	d.client = adapter.sdk
}

func (d *neonBranchesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	var data neonBranchesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.ListProjectBranchesRespObj
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		result, err = d.client.ListProjectBranches(data.ProjectID.ValueString(), nil, nil, nil, nil, nil, nil)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	branches := make([]branchModel, 0, len(result.Branches))
	for _, branch := range result.Branches {
		parentID := ""
		if branch.ParentID != nil {
			parentID = *branch.ParentID
		}
		logicalSize := int64(0)
		if branch.LogicalSize != nil {
			logicalSize = *branch.LogicalSize
		}
		primary := false
		if branch.Primary != nil {
			primary = *branch.Primary
		}
		branches = append(branches, branchModel{
			ID:          types.StringValue(branch.ID),
			Name:        types.StringValue(branch.Name),
			ParentID:    types.StringValue(parentID),
			LogicalSize: types.Int64Value(logicalSize),
			Primary:     types.BoolValue(primary),
		})
	}

	branchList, diags := types.ListValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"id":           types.StringType,
			"name":         types.StringType,
			"parent_id":    types.StringType,
			"logical_size": types.Int64Type,
			"primary":      types.BoolType,
		},
	}, branches)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Branches = branchList
	data.ID = types.StringValue(data.ProjectID.ValueString() + "/branches")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
