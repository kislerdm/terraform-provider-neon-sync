package provider

import (
	"context"
	"net/url"

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
	ID        types.String  `tfsdk:"id"`
	ProjectID types.String  `tfsdk:"project_id"`
	Branches  []branchModel `tfsdk:"branches"`
}

type branchModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ParentID    types.String `tfsdk:"parent_id"`
	LogicalSize types.Int64  `tfsdk:"logical_size"`
	Primary     types.Bool   `tfsdk:"primary"`
	Default     types.Bool   `tfsdk:"default"`
	Protected   types.Bool   `tfsdk:"protected"`
}

func NewBranchesDataSource() datasource.DataSource {
	return &neonBranchesDataSource{}
}

func (d *neonBranchesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_branches"
}

func (d *neonBranchesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetch Project Branches.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Project ID.",
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
							Computed:           true,
							Description:        "Primary branch flag.",
							DeprecationMessage: "Use default instead.",
						},
						"default": schema.BoolAttribute{
							Computed:    true,
							Description: "Default branch flag.",
						},
						"protected": schema.BoolAttribute{
							Computed:    true,
							Description: "Protected branch flag.",
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

	data.Branches = make([]branchModel, 0)
	var cursor *string
	var sortBy *neon.ListProjectBranchesSortBy
	var sortOrder *neon.SortOrderParam
	for {
		if err := ctx.Err(); err != nil {
			resp.Diagnostics.Append(frameworkDiagnostics(err)...)
			return
		}
		var result neon.ListProjectBranchesRespObj
		resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
			var err error
			result, err = d.client.ListProjectBranches(
				data.ProjectID.ValueString(), nil, sortBy, cursor, sortOrder, nil, nil,
			)
			return err
		}, ctx)...)
		if resp.Diagnostics.HasError() {
			return
		}

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
			data.Branches = append(data.Branches, branchModel{
				ID:          types.StringValue(branch.ID),
				Name:        types.StringValue(branch.Name),
				ParentID:    types.StringValue(parentID),
				LogicalSize: types.Int64Value(logicalSize),
				Primary:     types.BoolValue(primary),
				Default:     types.BoolValue(branch.Default),
				Protected:   types.BoolValue(branch.Protected),
			})
		}

		pagination := result.Pagination
		if pagination == nil || pagination.Next == nil || *pagination.Next == "" {
			break
		}
		if pagination.SortBy != nil {
			value, err := neon.NewListProjectBranchesSortBy(*pagination.SortBy)
			if err != nil {
				resp.Diagnostics.Append(frameworkDiagnostics(err)...)
				return
			}
			sortBy = &value
		}
		if pagination.SortOrder != nil {
			value, err := neon.NewSortOrderParam(*pagination.SortOrder)
			if err != nil {
				resp.Diagnostics.Append(frameworkDiagnostics(err)...)
				return
			}
			sortOrder = &value
		}
		next := url.QueryEscape(*pagination.Next)
		cursor = &next
	}

	data.ID = data.ProjectID
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
