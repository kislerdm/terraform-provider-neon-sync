package provider

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int32planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonProjectResource)(nil)
var _ resource.ResourceWithImportState = (*neonProjectResource)(nil)

func NewNeonProjectResource() resource.Resource {
	return &neonProjectResource{}
}

func findDefaultDatabase(databases []neon.Database, defaultBranchID string) neon.Database {
	o := neon.Database{}

	if len(databases) > 0 {
		if len(databases) > 0 {
			var dbs []neon.Database
			for _, el := range databases {
				if el.BranchID == defaultBranchID {
					dbs = append(dbs, el)
				}
			}

			// select the default database based on the creation timestamp
			slices.SortStableFunc(dbs, func(a, b neon.Database) int {
				return a.CreatedAt.Compare(b.CreatedAt)
			})

			o = dbs[0]
		}
	}

	return o
}

func findDefaultEndpoint(endpoints []neon.Endpoint, defaultBranchID string) neon.Endpoint {
	o := neon.Endpoint{}

	if len(endpoints) > 0 {
		var eps []neon.Endpoint
		for _, el := range endpoints {
			// the default endpoint can only be of read_write type
			if !el.Disabled && el.Type == neon.EndpointTypeReadWrite && el.BranchID == defaultBranchID {
				eps = append(eps, el)
			}
		}

		// select the default endpoint based on the creation timestamp
		slices.SortStableFunc(eps, func(a, b neon.Endpoint) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		})

		o = eps[0]
	}

	return o
}

type neonProjectResourceModel struct {
	ID       types.String `tfsdk:"id"`
	OrgID    types.String `tfsdk:"org_id"`
	Name     types.String `tfsdk:"name"`
	RegionID types.String `tfsdk:"region_id"`

	PgVersion types.Int32 `tfsdk:"pg_version"`

	StorePassword types.Bool `tfsdk:"store_password"`

	AllowedIPs                      types.List `tfsdk:"allowed_ips"`
	AllowedIPsProtectedBranchesOnly types.Bool `tfsdk:"allowed_ips_protected_branches_only"`

	ComputeProvisioner    types.String  `tfsdk:"compute_provisioner"`
	AutoscalingLimitMaxCU types.Float64 `tfsdk:"autoscaling_limit_max_cu"`
	AutoscalingLimitMinCU types.Float64 `tfsdk:"autoscaling_limit_min_cu"`
	SuspendTimeoutSeconds types.Int64   `tfsdk:"suspend_timeout_seconds"`

	HistoryRetentionSeconds  types.Int32 `tfsdk:"history_retention_seconds"`
	EnableLogicalReplication types.Bool  `tfsdk:"enable_logical_replication"`

	BlockPublicConnections types.Bool `tfsdk:"block_public_connections"`
	BlockVPCConnections    types.Bool `tfsdk:"block_vpc_connections"`

	DatabaseName types.String `tfsdk:"database_name"`
	DatabaseUser types.String `tfsdk:"database_user"`

	EnabledLibraries            types.List `tfsdk:"enabled_libraries"`
	EnabledLibrariesUseDefaults types.Bool `tfsdk:"enabled_libraries_use_defaults"`

	Hipaa types.Bool `tfsdk:"hipaa"`

	MaintenanceWindow maintenanceWindow         `tfsdk:"maintenance_window"`
	Quota             projectQuota              `tfsdk:"quota"`
	Branch            neonBranchResourceModel   `tfsdk:"branch"`
	PrimaryCompute    neonEndpointResourceModel `tfsdk:"primary_compute"`

	// TODO: delete after the off. release
	DefaultBranchID types.String `tfsdk:"default_branch_id"`
	// TODO: delete after the off. release
	DefaultEndpointID types.String `tfsdk:"default_endpoint_id"`
	// TODO: delete after the off. release
	DefaultBranchProtected types.Bool `tfsdk:"default_branch_protected"`
	// TODO: delete after the off. release
	ConnectionURI types.String `tfsdk:"connection_uri"`
	// TODO: delete after the off. release
	ConnectionURIPooler types.String `tfsdk:"connection_uri_pooler"`
	// TODO: delete after the off. release
	DatabasePassword types.String `tfsdk:"database_password"`
	// TODO: delete after the off. release
	DatabaseHost types.String `tfsdk:"database_host"`
	// TODO: delete after the off. release
	DatabaseHostPooler types.String `tfsdk:"database_host_pooler"`

	// 	TODO: add pgSettings (?) verify for bug in API behaviour
}

func (v *neonProjectResourceModel) inferAttr(ctx context.Context, project neon.Project, branch neon.Branch,
	endpoint neon.Endpoint, database neon.Database, diagnostics *diag.Diagnostics) {
	v.ID = types.StringValue(project.ID)
	v.Name = types.StringValue(project.Name)
	v.OrgID = types.StringPointerValue(project.OrgID)
	v.RegionID = types.StringValue(project.RegionID)
	v.ComputeProvisioner = types.StringValue(string(project.Provisioner))
	v.PgVersion = types.Int32Value(int32(project.PgVersion))
	v.StorePassword = types.BoolValue(project.StorePasswords)
	v.HistoryRetentionSeconds = types.Int32Value(project.HistoryRetentionSeconds)

	v.EnableLogicalReplication = types.BoolUnknown()
	v.BlockPublicConnections = types.BoolUnknown()
	v.BlockVPCConnections = types.BoolUnknown()
	v.EnableLogicalReplication = types.BoolUnknown()
	v.Hipaa = types.BoolUnknown()

	v.AllowedIPsProtectedBranchesOnly = types.BoolUnknown()
	v.AllowedIPs = types.ListUnknown(types.StringType)

	v.Quota.ActiveTimeSeconds = types.Int64Unknown()
	v.Quota.ComputeTimeSeconds = types.Int64Unknown()
	v.Quota.DataTransferBytes = types.Int64Unknown()
	v.Quota.LogicalSizeBytes = types.Int64Unknown()
	v.Quota.WrittenDataBytes = types.Int64Unknown()

	v.EnabledLibraries = types.ListUnknown(types.StringType)
	v.EnabledLibrariesUseDefaults = types.BoolUnknown()

	if project.Settings != nil {
		settings := project.Settings

		v.Hipaa = types.BoolPointerValue(settings.Hipaa)
		v.EnableLogicalReplication = types.BoolPointerValue(settings.EnableLogicalReplication)
		v.BlockPublicConnections = types.BoolPointerValue(settings.BlockPublicConnections)
		v.BlockVPCConnections = types.BoolPointerValue(settings.BlockVpcConnections)
		v.EnableLogicalReplication = types.BoolPointerValue(settings.EnableLogicalReplication)

		var d diag.Diagnostics
		if settings.AllowedIps != nil {
			v.AllowedIPsProtectedBranchesOnly = types.BoolPointerValue(settings.AllowedIps.ProtectedBranchesOnly)
			v.AllowedIPs, d = types.ListValueFrom(ctx, types.StringType, settings.AllowedIps.Ips)
			diagnostics.Append(d...)
		}

		if settings.Quota != nil {
			v.Quota.ActiveTimeSeconds = types.Int64PointerValue(settings.Quota.ActiveTimeSeconds)
			v.Quota.ComputeTimeSeconds = types.Int64PointerValue(settings.Quota.ComputeTimeSeconds)
			v.Quota.DataTransferBytes = types.Int64PointerValue(settings.Quota.DataTransferBytes)
			v.Quota.LogicalSizeBytes = types.Int64PointerValue(settings.Quota.LogicalSizeBytes)
			v.Quota.WrittenDataBytes = types.Int64PointerValue(settings.Quota.WrittenDataBytes)
		}

		if settings.PreloadLibraries != nil {
			v.EnabledLibraries, d = types.ListValueFrom(ctx, types.StringType, settings.PreloadLibraries.EnabledLibraries)
			diagnostics.Append(d...)
			v.EnabledLibrariesUseDefaults = types.BoolPointerValue(settings.PreloadLibraries.UseDefaults)
		}

		if settings.MaintenanceWindow != nil {
			v.MaintenanceWindow = maintenanceWindow{
				Weekdays:  settings.MaintenanceWindow.Weekdays,
				StartTime: types.StringValue(settings.MaintenanceWindow.StartTime),
				EndTime:   types.StringValue(settings.MaintenanceWindow.EndTime),
			}
		}
	}

	// default compute a/k/a endpoints settings
	v.AutoscalingLimitMinCU = types.Float64Unknown()
	v.AutoscalingLimitMaxCU = types.Float64Unknown()
	v.SuspendTimeoutSeconds = types.Int64Unknown()
	if project.DefaultEndpointSettings != nil {
		settings := project.DefaultEndpointSettings
		if settings.AutoscalingLimitMaxCu != nil {
			v.AutoscalingLimitMaxCU = types.Float64Value(float64(*settings.AutoscalingLimitMaxCu))
		}
		if settings.AutoscalingLimitMinCu != nil {
			v.AutoscalingLimitMinCU = types.Float64Value(float64(*settings.AutoscalingLimitMinCu))
		}
		if settings.SuspendTimeoutSeconds != nil {
			v.SuspendTimeoutSeconds = types.Int64Value(int64(*settings.SuspendTimeoutSeconds))
		}
	}

	v.Branch.inferAttr(branch)
	v.PrimaryCompute.inferAttr(ctx, endpoint, diagnostics)

	v.DatabaseName = types.StringValue(database.Name)
	v.DatabaseUser = types.StringValue(database.OwnerName)

	// TODO: remove after the off. release
	{
		v.DefaultBranchID = v.Branch.ID
		v.DefaultBranchProtected = v.Branch.Protected
		v.DefaultEndpointID = v.PrimaryCompute.ID
		v.DatabaseHost = v.PrimaryCompute.Host
		v.DatabaseHostPooler = v.PrimaryCompute.HostPooler
	}
}

type maintenanceWindow struct {
	Weekdays  []int        `tfsdk:"weekdays"`
	StartTime types.String `tfsdk:"start_time"`
	EndTime   types.String `tfsdk:"end_time"`
}

type validatorMaintenanceWindow struct{}

func (v validatorMaintenanceWindow) Description(_ context.Context) string {
	return "Maintenance window's validator."
}

func (v validatorMaintenanceWindow) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validatorMaintenanceWindow) ValidateObject(ctx context.Context, req validator.ObjectRequest,
	resp *validator.ObjectResponse) {
	var data neonProjectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var isValidTime = func(s string) bool {
		_, err := time.Parse("15:04", s)
		return err == nil
	}

	window := data.MaintenanceWindow
	errMsg := "undefined maintenance window"
	switch {
	case len(window.Weekdays) > 7 || len(window.Weekdays) < 1:
		resp.Diagnostics.AddError(errMsg, "invalid number of weekdays, it must be between 1 and 7")
	default:
		for j, weekday := range window.Weekdays {
			switch weekday {
			case 1, 2, 3, 4, 5, 6, 7:
			default:
				resp.Diagnostics.AddError(errMsg,
					fmt.Sprintf("invalid weekday %d, the value must be from 1 (Mon.) to 7 (Sun.)", j))
			}
		}
	}
	if window.StartTime.IsUnknown() || window.StartTime.IsNull() {
		resp.Diagnostics.AddError(errMsg, "start_time is required")
	}
	if window.EndTime.IsUnknown() || window.EndTime.IsNull() {
		resp.Diagnostics.AddError(errMsg, "end_time is required")
	}

	if !isValidTime(window.StartTime.ValueString()) {
		resp.Diagnostics.AddError(errMsg, "invalid start_time, it must be in the format of \"HH:MM\"")
	}
	if !isValidTime(window.EndTime.ValueString()) {
		resp.Diagnostics.AddError(errMsg, "invalid end_time, it must be in the format of \"HH:MM\"")
	}
}

type projectQuota struct {
	ActiveTimeSeconds  types.Int64 `tfsdk:"active_time_seconds"`
	ComputeTimeSeconds types.Int64 `tfsdk:"compute_time_seconds"`
	WrittenDataBytes   types.Int64 `tfsdk:"written_data_bytes"`
	DataTransferBytes  types.Int64 `tfsdk:"data_transfer_bytes"`
	LogicalSizeBytes   types.Int64 `tfsdk:"logical_size_bytes"`
}

type neonProjectResource struct {
	client *neon.Client
}

func (r neonProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_project"
}

func (r neonProjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	r.client = client.sdk
}

func (r neonProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	atts := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "Project ID.",
			Computed:    true,
		},
		"org_id": schema.StringAttribute{
			Description:   "Identifier of the organisation to which this project belongs.",
			Computed:      true,
			Optional:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"name": schema.StringAttribute{
			Description: "Project name.",
			Optional:    true,
			Computed:    true,
		},
		"region_id": schema.StringAttribute{
			Description:   "Deployment region: https://neon.com/docs/introduction/regions",
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"pg_version": schema.Int32Attribute{
			Description:   "Postgres version",
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.Int32{int32planmodifier.RequiresReplace()},
			// 	TODO: add validator
		},
		"store_password": schema.BoolAttribute{
			Description: "Whether or not passwords are stored for roles in the Neon project.\nStoring passwords facilitates access to Neon features that require authorization.",
			Optional:    true,
			Default:     booldefault.StaticBool(true),
		},
		"history_retention_seconds": schema.Int32Attribute{
			Description: "The number of seconds to retain the point-in-time restore (PITR) backup history for this project.\nDefault: 1 day, see https://neon.com/docs/reference/glossary#point-in-time-restore.",
			Optional:    true,
			Computed:    true,
		},
		"allowed_ips": schema.ListAttribute{
			Description: "A list of IP addresses that are allowed to connect to the endpoints.\nNote that the feature is available to the Neon Scale plans only. Details: https://neon.com/docs/manage/projects#configure-ip-allow",
			Optional:    true,
			ElementType: types.StringType,
		},
		"allowed_ips_protected_branches_only": schema.BoolAttribute{
			Description: "Apply the allow-list to the protected branches only.\nNote that the feature is available to the Neon Scale plans only.",
			Optional:    true,
			Computed:    true,
		},
		"block_public_connections": schema.BoolAttribute{
			Description: "Block connections from public internet. This supersedes the AllowedIPs list.",
			Optional:    true,
			Computed:    true,
		},
		"block_vpc_connections": schema.BoolAttribute{
			Description: "Block connections that use VPC endpoints.",
			Optional:    true,
			Computed:    true,
		},
		"enable_logical_replication": schema.BoolAttribute{
			Description: "Sets wal_level=logical for all compute endpoints in this project.\nAll active endpoints will be suspended. See details: https://neon.com/docs/introduction/logical-replication\n**Note**: removal of this config from the manifest will correspond to the API call with the respective setting set to false.\n\n**Warning**: Once enabled, logical replication cannot be disabled.",
			Optional:    true,
			Computed:    true,
		},
		"database_name": schema.StringAttribute{
			Description: "Default database name.",
			Computed:    true,
		},
		"database_user": schema.StringAttribute{
			Description: "Default database role.",
			Computed:    true,
		},
		"autoscaling_limit_min_cu": schema.Float64Attribute{
			Description: "Minimal value of the project-wide autoscaling limit.\nNote that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.",
			Optional:    true,
			Computed:    true,
		},
		"autoscaling_limit_max_cu": schema.Float64Attribute{
			Description: "Maximal value of the project-wide autoscaling limit.\nNote that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.",
			Optional:    true,
			Computed:    true,
		},
		"suspend_timeout_seconds": schema.Int64Attribute{
			Description: "Project-wide scale-to-zero idle timeout, in seconds, before the compute suspends." +
				"\nNote that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.\n" +
				"   - `0` means the plan default (`300` sec.) applies;\n" +
				"   - `-1` means scale-to-zero is disabled (never suspends), can be set only on paid plans;\n" +
				"   - `604800` sec. (1 week) is the maximum.\n" +
				"   - `60` sec. (1 minute) is the minimum.\n" +
				"See details [here](https://neon.com/docs/manage/endpoints#scale-to-zero-configuration).",
			Optional:   true,
			Computed:   true,
			Validators: []validator.Int64{validateSuspendTimeout},
		},
		"hipaa": schema.BoolAttribute{
			Description:   "Enable HIPAA compliance for the project.\nNote that HIPAA must be configured for the organization first.\n\n**Warning**: Once enabled, HIPAA cannot be disabled.",
			Computed:      true,
			Optional:      true,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
		},
		"enabled_libraries": schema.ListAttribute{
			Description: "Names of shared libraries preload by default when provisioning compute resources, i.e., endpoints.",
			Optional:    true,
			Computed:    true,
			ElementType: types.StringType,
		},
		"enabled_libraries_use_defaults": schema.BoolAttribute{
			Description: "When true, the project's preload libraries include the platform default set in addition to any libraries listed in `enabled_libraries`.",
			Optional:    true,
			Computed:    true,
		},
	}

	// TODO: delete after the off. release
	attrToDeleteAfterOffRelease := map[string]schema.Attribute{
		"default_branch_id": schema.StringAttribute{
			Description:        "Default branch ID.",
			DeprecationMessage: "Use the `id` attribute in the block `branch` instead.",
			Computed:           true,
		},
		"default_branch_protected": schema.BoolAttribute{
			Description:        "Set default branch as protected. **Note** that the default value is false.",
			DeprecationMessage: "Use the `protected` attribute in the block `branch` instead.",
			Optional:           true,
			Default:            booldefault.StaticBool(false),
		},
		"default_endpoint_id": schema.StringAttribute{
			Description:        "Default endpoint ID.",
			DeprecationMessage: "Use the `id` attribute in the block `primary_compute` instead.",
			Computed:           true,
		},
		"database_host": schema.StringAttribute{
			Description:        "The hostname of the default compute endpoint.",
			DeprecationMessage: "Use the `host` attribute in the block `primary_compute` instead.",
			Computed:           true,
		},
		"database_host_pooler": schema.StringAttribute{
			Description:        "The hostname of the default compute endpoint accessible through the connection pool (pgbouncer).",
			DeprecationMessage: "Use the `host_pooler` attribute in the block `primary_compute` instead.",
			Computed:           true,
		},
		"database_password": schema.StringAttribute{
			Description:        "The password for the default database user.",
			DeprecationMessage: "The attribute is always empty, use the ephemeral resource `neon_role` instead.",
			Computed:           true,
			Sensitive:          true,
			Default:            stringdefault.StaticString(""),
		},
		"connection_uri": schema.StringAttribute{
			Description:        "Default connection uri.",
			DeprecationMessage: "The attribute is always empty, use the ephemeral resource `neon_connection_uri` instead.",
			Computed:           true,
			Sensitive:          true,
			Default:            stringdefault.StaticString(""),
		},
		"connection_uri_pooler": schema.StringAttribute{
			Description:        "Default connection uri with the traffic through the connection pool (pgbouncer).",
			DeprecationMessage: "The attribute is always empty, use the ephemeral resource `neon_connection_uri` instead.",
			Computed:           true,
			Sensitive:          true,
			Default:            stringdefault.StaticString(""),
		},
	}
	maps.Copy(atts, attrToDeleteAfterOffRelease)

	resp.Schema = schema.Schema{
		Description: `Neon Project.

Note that this resource manages five resources:

- Project: https://neon.com/docs/reference/glossary#project
- Root branch: https://neon.com/docs/reference/glossary#root-branch
- Primary compute with the default read-write endpoint: https://neon.com/docs/reference/glossary#compute
- Default database: https://neon.com/docs/reference/glossary#database
- Default role: https://neon.com/docs/reference/glossary#postgres-role
`,
		Attributes: atts,
		Blocks: map[string]schema.Block{
			"branch": schema.SingleNestedBlock{
				Description: "Main branch provisioned with the project.",
				Attributes:  branchSchemaAttributes,
			},
			"primary_compute": schema.SingleNestedBlock{
				Description: "Primary read-write compute resource provisioned with the project.",
				Attributes:  endpointSchemaAttributes,
			},
			"quota": schema.SingleNestedBlock{
				Description: "Per-project consumption quota. If the quota is exceeded, all active computes\nare automatically suspended and it will not be possible to start them with\nan API method call or incoming proxy connections. The only exception is\nlogical_size_bytes, which is applied on per-branch basis, i.e., only the\ncompute on the branch that exceeds the logical_size quota will be suspended.\n\nQuotas are enforced based on per-project consumption metrics with the same names,\nwhich are reset at the end of each billing period (the first day of the month).\nLogical size is also an exception in this case, as it represents the total size\nof data stored in a branch, so it is not reset.\n\nThe zero value per attributed means 'unlimited'.",
				Attributes: map[string]schema.Attribute{
					"active_time_seconds": schema.Int64Attribute{
						Description: "The total amount of wall-clock time allowed to be spent by the project's compute endpoints.",
						Optional:    true,
						Computed:    true,
					},
					"compute_time_seconds": schema.Int64Attribute{
						Description: "The total amount of CPU seconds allowed to be spent by the project's compute endpoints.",
						Optional:    true,
						Computed:    true,
					},
					"written_data_bytes": schema.Int64Attribute{
						Description: "Total amount of data written to all of a project's branches.",
						Optional:    true,
						Computed:    true,
					},
					"data_transfer_bytes": schema.Int64Attribute{
						Description: "Total amount of data transferred from all of a project's branches using the proxy.",
						Optional:    true,
						Computed:    true,
					},
					"logical_size_bytes": schema.Int64Attribute{
						Description: "Limit on the logical size of every project's branch.",
						Optional:    true,
						Computed:    true,
					},
				},
			},
			"maintenance_window": schema.SingleNestedBlock{
				Description: "A time period during which Neon may perform maintenance on the project's infrastructure. During this time, the project's compute endpoints may be unavailable and existing connections can be interrupted.",
				Validators:  []validator.Object{validatorMaintenanceWindow{}},
				Attributes: map[string]schema.Attribute{
					"weekdays": schema.ListAttribute{
						Description: "A list of weekdays when the maintenance window is active. Encoded as ints, where 1 - Monday, and 7 - Sunday.",
						Required:    true,
						ElementType: types.Int32Type,
					},
					"start_time": schema.StringAttribute{
						Description: "Start time of the maintenance window, in the format of \"HH:MM\". Uses UTC.",
						Required:    true,
					},
					"end_time": schema.StringAttribute{
						Description: "End time of the maintenance window, in the format of \"HH:MM\". Uses UTC.",
						Required:    true,
					},
				},
			},
		},
	}
}

func (r neonProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var cfg neon.ProjectCreateRequest
	// TODO: add the request config inference

	var project neon.Project
	var defaultBranch neon.Branch
	var endpoints []neon.Endpoint
	var databases []neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
		result, err := r.client.CreateProject(cfg)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
		project = result.ProjectResponse.Project
		defaultBranch = result.BranchResponse.Branch
		endpoints = result.EndpointsResponse.Endpoints
		databases = result.DatabasesResponse.Databases
		return nil
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	defaultEndpoint := findDefaultEndpoint(endpoints, defaultBranch.ID)
	defaultDatabase := findDefaultDatabase(databases, defaultBranch.ID)

	state.inferAttr(ctx, project, defaultBranch, defaultEndpoint, defaultDatabase, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r neonProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var project neon.Project
	var defaultBranch neon.Branch
	var defaultEndpoint neon.Endpoint
	var defaultDatabase neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		// read the project details
		result, err := r.client.GetProject(state.ID.ValueString())
		if err != nil {
			return err
		}
		project = result.Project

		// read the default branch details
		var cursor *string
		for {
			res, err := r.client.ListProjectBranches(project.ID, nil,
				&neon.ListProjectBranchesSortByCreatedAt, cursor, &neon.SortOrderParamAsc,
				nil, nil)
			if err != nil {
				return err
			}

			if res.CursorPaginationResponse.Pagination != nil {
				cursor = res.CursorPaginationResponse.Pagination.Next
			} else {
				cursor = nil
			}

			for _, br := range res.Branches {
				if br.Default {
					defaultBranch = br
					cursor = nil
					break
				}
			}

			if cursor == nil {
				break
			}
		}

		if defaultBranch.ID == "" {
			return nil
		}

		var errs = make(chan error, 2)
		// read default endpoint
		go func() {
			resEndpoints, err := r.client.ListProjectEndpoints(project.ID)
			if err != nil {
				errs <- err
				return
			}
			defaultEndpoint = findDefaultEndpoint(resEndpoints.Endpoints, defaultBranch.ID)
			errs <- nil
		}()

		// read default database
		go func() {
			resDatabases, err := r.client.ListProjectBranchDatabases(project.ID, defaultBranch.ID)
			if err != nil {
				errs <- err
				return
			}
			defaultDatabase = findDefaultDatabase(resDatabases.Databases, defaultBranch.ID)
			errs <- nil
		}()

		for i := 0; i < cap(errs); i++ {
			if e := <-errs; e != nil {
				return e
			}
		}

		return nil
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	if project.ID == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(ctx, project, defaultBranch, defaultEndpoint, defaultDatabase, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r neonProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// TODO implement me
	panic("implement me")
}
func (r neonProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// TODO implement me
	panic("implement me")
}

func (r neonProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// TODO implement me
	panic("implement me")
}
