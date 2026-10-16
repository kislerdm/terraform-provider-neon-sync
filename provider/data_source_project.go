package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonProjectDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonProjectDataSource)(nil)

type neonProjectDataSource struct {
	client *neon.Client
}

type neonProjectDataSourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	DefaultBranchID     types.String `tfsdk:"default_branch_id"`
	DatabaseHost        types.String `tfsdk:"database_host"`
	DatabaseHostPooler  types.String `tfsdk:"database_host_pooler"`
	DatabaseName        types.String `tfsdk:"database_name"`
	DatabaseUser        types.String `tfsdk:"database_user"`
	DatabasePassword    types.String `tfsdk:"database_password"`
	ConnectionURI       types.String `tfsdk:"connection_uri"`
	ConnectionURIPooler types.String `tfsdk:"connection_uri_pooler"`
}

func NewProjectDataSource() datasource.DataSource {
	return &neonProjectDataSource{}
}

func (d *neonProjectDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_project"
}

func (d *neonProjectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetch a project and connection details from its first primary branch. Branch selection uses one API response without pagination. Credentials are stored in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Project name.",
			},
			"default_branch_id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the first branch with the legacy primary flag set.",
			},
			"database_host": schema.StringAttribute{
				Computed:    true,
				Description: "Host of the oldest enabled read-write endpoint on the primary branch.",
			},
			"database_host_pooler": schema.StringAttribute{
				Computed:    true,
				Description: "Selected endpoint host via pooler.",
			},
			"database_name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the oldest database on the primary branch.",
			},
			"database_user": schema.StringAttribute{
				Computed:    true,
				Description: "Owner of the selected database.",
			},
			"database_password": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Selected database owner's password. Requires permission to reveal the password.",
			},
			"connection_uri": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Selected database connection URI. **Note** that it contains access credentials.",
			},
			"connection_uri_pooler": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Selected database connection URI via pooler. **Note** that it contains access credentials.",
			},
		},
	}
}

func (d *neonProjectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *neonProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	var data neonProjectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var project neon.ProjectResponse
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		project, err = d.client.GetProject(data.ID.ValueString())
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectID := project.Project.ID
	var branches neon.ListProjectBranchesRespObj
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		branches, err = d.client.ListProjectBranches(projectID, nil, nil, nil, nil, nil, nil)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var branchID string
	for _, branch := range branches.Branches {
		if branch.Primary != nil && *branch.Primary {
			branchID = branch.ID
			break
		}
	}
	if branchID == "" {
		resp.Diagnostics.AddError("No primary branch found", "The project branch response contains no selectable primary branch. Check the project's primary branch before reading connection details.")
		return
	}

	var endpoints neon.EndpointsResponse
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		endpoints, err = d.client.ListProjectBranchEndpoints(projectID, branchID)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var databases neon.DatabasesResponse
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		databases, err = d.client.ListProjectBranchDatabases(projectID, branchID)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var endpoint *neon.Endpoint
	for i := range endpoints.Endpoints {
		candidate := &endpoints.Endpoints[i]
		if candidate.BranchID != branchID || candidate.Disabled || candidate.Type != neon.EndpointTypeReadWrite {
			continue
		}
		if endpoint == nil || candidate.CreatedAt.Before(endpoint.CreatedAt) {
			endpoint = candidate
		}
	}
	if endpoint == nil {
		resp.Diagnostics.AddError("No eligible endpoint found", "The primary branch has no enabled read-write endpoint. Configure one before reading connection details.")
		return
	}

	var database *neon.Database
	for i := range databases.Databases {
		candidate := &databases.Databases[i]
		if candidate.BranchID != branchID {
			continue
		}
		if database == nil || candidate.CreatedAt.Before(database.CreatedAt) {
			database = candidate
		}
	}
	if database == nil {
		resp.Diagnostics.AddError("No matching database found", "The primary branch has no matching database. Configure one before reading connection details.")
		return
	}

	var password neon.RolePasswordResponse
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		password, err = d.client.GetProjectBranchRolePassword(projectID, branchID, database.OwnerName)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	info := dbConnectionInfo{
		userName:   database.OwnerName,
		pass:       password.Password,
		dbName:     database.Name,
		host:       endpoint.Host,
		poolerHost: newPooledHost(endpoint.Host),
	}
	data.Name = types.StringValue(project.Project.Name)
	data.DefaultBranchID = types.StringValue(branchID)
	data.DatabaseHost = types.StringValue(info.host)
	data.DatabaseHostPooler = types.StringValue(info.poolerHost)
	data.DatabaseName = types.StringValue(info.dbName)
	data.DatabaseUser = types.StringValue(info.userName)
	data.DatabasePassword = types.StringValue(info.pass)
	data.ConnectionURI = types.StringValue(info.connectionURI())
	data.ConnectionURIPooler = types.StringValue(info.poolerConnectionURI())
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
