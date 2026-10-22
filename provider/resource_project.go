package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	diagSDK "github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	schemaSDK "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"
	typesSDK "github.com/neondatabase/terraform-provider-neon/provider/types"
)

const providerDefaultHistoryRetentionSeconds = int(time.Hour/time.Second) * 24

func newStoreProjectPasswordDefault() *schemaSDK.Schema {
	o := typesSDK.NewOptionalTristateBool(`Whether or not passwords are stored for roles in the Neon project.
Storing passwords facilitates access to Neon features that require authorization.`, false)
	o.Default = typesSDK.ValTrue
	return o
}

var schemaCompute = &schemaSDK.Schema{
	Type:        schemaSDK.TypeList,
	MaxItems:    1,
	Computed:    true,
	Optional:    true,
	Description: `Primary read-write compute resource provisioned with the project.`,
	Elem: &schemaSDK.Resource{
		Schema: map[string]*schemaSDK.Schema{
			"id": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Endpoint ID.",
			},
			"autoscaling_limit_min_cu": {
				Type:        schemaSDK.TypeFloat,
				Optional:    true,
				Computed:    true,
				Description: `Minimal value of the autoscaling limit for the primary project compute.`,
			},
			"autoscaling_limit_max_cu": {
				Type:        schemaSDK.TypeFloat,
				Optional:    true,
				Computed:    true,
				Description: `Maximal value of the autoscaling limit for the primary project compute.`,
			},
			"suspend_timeout_seconds": {
				Type:     schemaSDK.TypeInt,
				Optional: true,
				Computed: true,
				ValidateFunc: func(v interface{}, s string) (warn []string, errs []error) {
					switch vv, ok := v.(int); ok {
					case vv == -1:
					default:
						warn, errs = intValidationNotNegative(v, s)
					}
					return warn, errs
				},
				Description: `Duration of inactivity in seconds after which the default compute endpoint is automatically suspended.
The value 0 means use the global default.
The value -1 means never suspend. The default value is 300 seconds (5 minutes).
The maximum value is 604800 seconds (1 week)`,
			},
			"name": {
				Type:        schemaSDK.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Compute name.",
			},
		},
	},
}

func resourceProject() *schemaSDK.Resource {
	maintenanceWindowSettings := &schemaSDK.Schema{
		Type:     schemaSDK.TypeList,
		MaxItems: 1,
		Computed: true,
		Optional: true,
		Description: "A time period during which Neon may perform maintenance on the project's infrastructure. " +
			"During this time, the project's compute endpoints may be unavailable and existing " +
			"connections can be interrupted.",
		Elem: &schemaSDK.Resource{
			Schema: map[string]*schemaSDK.Schema{
				"weekdays": {
					Type:     schemaSDK.TypeList,
					Required: true,
					MinItems: 1,
					MaxItems: 7,
					Description: "A list of weekdays when the maintenance window is active. " +
						"Encoded as ints, where 1 - Monday, and 7 - Sunday.",
					Elem: &schemaSDK.Schema{
						Type: schemaSDK.TypeInt,
					},
				},
				"start_time": {
					Type:        schemaSDK.TypeString,
					Required:    true,
					Description: "Start time of the maintenance window, in the format of \"HH:MM\". Uses UTC.",
				},
				"end_time": {
					Type:        schemaSDK.TypeString,
					Required:    true,
					Description: "End time of the maintenance window, in the format of \"HH:MM\". Uses UTC.",
				},
			},
		},
	}
	return &schemaSDK.Resource{
		Description: `Neon Project.

Note that this resource manages five resources:

- Project: https://neon.com/docs/reference/glossary#project
- Root branch: https://neon.com/docs/reference/glossary#root-branch
- Primary compute with the default read-write endpoint: https://neon.com/docs/reference/glossary#compute
- Default database: https://neon.com/docs/reference/glossary#database
- Default role: https://neon.com/docs/reference/glossary#postgres-role
`,
		SchemaVersion: 11,
		Importer: &schemaSDK.ResourceImporter{
			StateContext: resourceProjectImport,
		},
		CreateContext: resourceProjectCreateRetry,
		ReadContext:   resourceProjectReadRetry,
		UpdateContext: resourceProjectUpdateRetry,
		DeleteContext: resourceProjectDeleteRetry,
		Schema: map[string]*schemaSDK.Schema{
			"id": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Project ID.",
			},
			"org_id": {
				Type:     schemaSDK.TypeString,
				Optional: true,
				ForceNew: true,
				DiffSuppressFunc: func(k, old, new string, d *schemaSDK.ResourceData) bool {
					var f = true
					switch {
					case new == old:
					case new == "" && old == d.Get(k).(string):
					default:
						f = false
					}
					return f
				},
				Description: "Identifier of the organisation to which this project belongs.",
			},
			"name": {
				Type:        schemaSDK.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Project name.",
			},
			"region_id": schemaRegionID,
			"pg_version": {
				Type:        schemaSDK.TypeInt,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Postgres version",
			},
			"store_password": newStoreProjectPasswordDefault(),
			"history_retention_seconds": {
				Type:         schemaSDK.TypeInt,
				Optional:     true,
				Default:      providerDefaultHistoryRetentionSeconds,
				ValidateFunc: intValidationNotNegative,
				Description: `The number of seconds to retain the point-in-time restore (PITR) backup history for this project.
Default: 1 day, see https://neon.tech/docs/reference/glossary#point-in-time-restore.`,
			},
			"compute_provisioner": {
				Type:     schemaSDK.TypeString,
				Optional: true,
				Computed: true,
				Description: `Provisioner The Neon compute provisioner.
Specify the k8s-neonvm provisioner to create a compute endpoint that supports Autoscaling.
`,
				ValidateFunc: func(i interface{}, s string) (warns []string, errs []error) {
					switch v := i.(string); v {
					case "k8s-pod", "k8s-neonvm":
					default:
						errs = append(
							errs,
							errors.New(
								v+" is not supported for "+s+
									". See details: https://api-docs.neon.tech/reference/createproject",
							),
						)
					}
					return
				},
			},
			"quota":  schemaSDKQuota,
			"branch": schemaSDKDefaultBranch,
			"allowed_ips": {
				Type:     schemaSDK.TypeList,
				MinItems: 1,
				Optional: true,
				Elem:     &schemaSDK.Schema{Type: schemaSDK.TypeString},
				Description: `A list of IP addresses that are allowed to connect to the endpoints.
Note that the feature is available to the Neon Scale plans only. Details: https://neon.tech/docs/manage/projects#configure-ip-allow`,
			},
			"allowed_ips_protected_branches_only": typesSDK.NewOptionalTristateBool(
				`Apply the allow-list to the protected branches only.
Note that the feature is available to the Neon Scale plans only.`, false),
			"block_public_connections": typesSDK.NewOptionalTristateBool(
				`Block connections from public internet. This supersedes the AllowedIPs list.`,
				false),
			"block_vpc_connections": typesSDK.NewOptionalTristateBool(
				`Block connections that use VPC endpoints.`, false),
			"enable_logical_replication": typesSDK.NewOptionalTristateBool(
				`Sets wal_level=logical for all compute endpoints in this project.
All active endpoints will be suspended. See details: https://neon.tech/docs/introduction/logical-replication
**Note**: removal of this config from the manifest will correspond to the API call with the respective setting set to false. 

**Warning**: Once enabled, logical replication cannot be disabled.`, false),
			// computed fields
			"default_branch_id": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Default branch ID.",
			},
			"database_host": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Default database host.",
			},
			"database_host_pooler": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Default endpoint host via pooler.",
			},
			"database_name": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Default database name.",
			},
			"database_user": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Default database role.",
			},
			"database_password": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Sensitive:   true,
				Description: "Default database access password.",
			},
			"connection_uri": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Sensitive:   true,
				Description: "Default connection uri. **Note** that it contains access credentials.",
			},
			"connection_uri_pooler": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Sensitive:   true,
				Description: "Default connection uri with the traffic via pooler. **Note** that it contains access credentials.",
			},
			"default_endpoint_id": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Default endpoint ID.",
			},
			"maintenance_window": maintenanceWindowSettings,
			"hipaa": typesSDK.NewOptionalTristateBool(`Enable HIPAA compliance for the project. 
Note that HIPAA must be configured for the organization first.

**Warning**: Once enabled, HIPAA cannot be disabled.`, false),
			"default_branch_protected": {
				Type:        schemaSDK.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Set default branch as protected. **Note** that the default value is false.",
			},
			"primary_compute": schemaCompute,

			// project-wide config for the compute resources
			"autoscaling_limit_min_cu": {
				Type:     schemaSDK.TypeFloat,
				Optional: true,
				Computed: true,
				Description: `Minimal value of the project-wide autoscaling limit. 
Note that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.`,
			},
			"autoscaling_limit_max_cu": {
				Type:     schemaSDK.TypeFloat,
				Optional: true,
				Computed: true,
				Description: `Maximal value of the project-wide autoscaling limit. 
Note that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.`,
			},
			"suspend_timeout_seconds": {
				Type:     schemaSDK.TypeInt,
				Optional: true,
				Computed: true,
				ValidateFunc: func(v interface{}, s string) (warn []string, errs []error) {
					switch vv, ok := v.(int); ok {
					case vv == -1:
					default:
						warn, errs = intValidationNotNegative(v, s)
					}
					return warn, errs
				},
				Description: `Duration of inactivity in seconds after which the compute endpoint is automatically suspended.
The value 0 means use the global default.
The value -1 means never suspend. The default value is 300 seconds (5 minutes).
The maximum value is 604800 seconds (1 week)`,
			},
		},
	}
}

var schemaSDKQuota = &schemaSDK.Schema{
	Type:     schemaSDK.TypeList,
	MaxItems: 1,
	Optional: true,
	Computed: true,
	Description: `Per-project consumption quota. If the quota is exceeded, all active computes
are automatically suspended and it will not be possible to start them with
an API method call or incoming proxy connections. The only exception is
logical_size_bytes, which is applied on per-branch basis, i.e., only the
compute on the branch that exceeds the logical_size quota will be suspended.

Quotas are enforced based on per-project consumption metrics with the same names,
which are reset at the end of each billing period (the first day of the month).
Logical size is also an exception in this case, as it represents the total size
of data stored in a branch, so it is not reset.

The zero value per attributed means 'unlimited'.`,
	Elem: &schemaSDK.Resource{
		Schema: map[string]*schemaSDK.Schema{
			"active_time_seconds": {
				Type:         schemaSDK.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: intValidationNotNegative,
				Description:  `The total amount of wall-clock time allowed to be spent by the project's compute endpoints.`,
			},
			"compute_time_seconds": {
				Type:         schemaSDK.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: intValidationNotNegative,
				Description:  `The total amount of CPU seconds allowed to be spent by the project's compute endpoints.`,
			},
			"written_data_bytes": {
				Type:         schemaSDK.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: intValidationNotNegative,
				Description:  `Total amount of data written to all of a project's branches.`,
			},
			"data_transfer_bytes": {
				Type:         schemaSDK.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: intValidationNotNegative,
				Description:  `Total amount of data transferred from all of a project's branches using the proxy.`,
			},
			"logical_size_bytes": {
				Type:         schemaSDK.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: intValidationNotNegative,
				Description:  `Limit on the logical size of every project's branch.`,
			},
		},
	},
}

func mapToQuotaSettings(v map[string]interface{}) (o *neon.ProjectQuota) {
	o = &neon.ProjectQuota{}
	if v, ok := v["active_time_seconds"].(int); ok && v > 0 {
		o.ActiveTimeSeconds = pointer(int64(v))
	}

	if v, ok := v["compute_time_seconds"].(int); ok && v > 0 {
		o.ComputeTimeSeconds = pointer(int64(v))
	}

	if v, ok := v["written_data_bytes"].(int); ok && v > 0 {
		o.WrittenDataBytes = pointer(int64(v))
	}

	if v, ok := v["data_transfer_bytes"].(int); ok && v > 0 {
		o.DataTransferBytes = pointer(int64(v))
	}

	if v, ok := v["logical_size_bytes"].(int); ok && v > 0 {
		o.LogicalSizeBytes = pointer(int64(v))
	}
	return o
}

var schemaSDKDefaultBranch = &schemaSDK.Schema{
	Type:     schemaSDK.TypeList,
	MaxItems: 1,
	Optional: true,
	Computed: true,
	ForceNew: true,
	Elem: &schemaSDK.Resource{
		Schema: map[string]*schemaSDK.Schema{
			"id": {
				Type:        schemaSDK.TypeString,
				Computed:    true,
				Description: "Branch ID.",
			},
			"name": {
				Type:     schemaSDK.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
				Description: `The name of the default branch provisioned upon creation of new project.
If not specified, the default branch name will be used.`,
			},
			"role_name": {
				Type:     schemaSDK.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
				Description: `The name of the default role provisioned upon creation of new project.
If not specified, the default role name will be used.`,
			},
			"database_name": {
				Type:     schemaSDK.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
				Description: `The name of the default database provisioned upon creation of new project. It's owned by the default role (` + "`role_name`" + `).
If not specified, the default database name will be used.`,
			},
		},
	},
}

func mapToBranchSettings(v map[string]interface{}) *neon.ProjectCreateRequestProjectBranch {
	o := &neon.ProjectCreateRequestProjectBranch{}

	if v, ok := v["name"].(string); ok && v != "" {
		o.Name = pointer(v)
	}

	if v, ok := v["database_name"].(string); ok && v != "" {
		o.DatabaseName = pointer(v)
	}

	if v, ok := v["role_name"].(string); ok && v != "" {
		o.RoleName = pointer(v)
	}

	return o
}

func newDbConnectionInfo(
	c sdkProject,
	projectID string,
	branchID string,
	endpoints []neon.Endpoint,
	databases []neon.Database,
) (dbConnectionInfo, error) {
	defaultEndpoint := findDefaultEndpoint(endpoints, branchID)

	defaultDB := findDefaultDatabase(databases, branchID)

	resp, err := c.GetProjectBranchRolePassword(projectID, branchID, defaultDB.OwnerName)
	if err != nil {
		return dbConnectionInfo{}, err
	}
	defaultRolePass := resp.Password

	return dbConnectionInfo{
		userName:        defaultDB.OwnerName,
		pass:            defaultRolePass,
		dbName:          defaultDB.Name,
		host:            defaultEndpoint.Host,
		poolerHost:      newPooledHost(defaultEndpoint.Host),
		endpointID:      defaultEndpoint.ID,
		defaultEndpoint: defaultEndpoint,
	}, nil
}

func newPooledHost(host string) string {
	const poolerSuffix = "-pooler"
	els := strings.SplitN(host, ".", 2)
	var o string
	if len(els) == 2 {
		o = fmt.Sprintf("%s.%s", els[0]+poolerSuffix, els[1])
	}
	return o
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

type dbConnectionInfo struct {
	userName        string
	pass            string
	dbName          string
	host            string
	endpointID      string
	poolerHost      string
	defaultEndpoint neon.Endpoint
}

const sslMode = "?sslmode=require"

func (i dbConnectionInfo) connectionURI() string {
	if i.userName == "" || i.host == "" || i.dbName == "" || i.pass == "" {
		return ""
	}
	return "postgres://" + i.userName + ":" + i.pass + "@" + i.host + "/" + i.dbName + sslMode
}

func (i dbConnectionInfo) poolerConnectionURI() string {
	if i.userName == "" || i.poolerHost == "" || i.dbName == "" || i.pass == "" {
		return ""
	}
	return "postgres://" + i.userName + ":" + i.pass + "@" + i.poolerHost + "/" + i.dbName + sslMode
}

func updateStateProject(d *schemaSDK.ResourceData, r neon.Project, defaultBranchID, defaultBranchName string,
	dbConnectionInfo dbConnectionInfo, defaultBranchProtected bool) error {
	if r.OrgID != nil {
		if err := d.Set("org_id", *r.OrgID); err != nil {
			return err
		}
	}
	if err := d.Set("name", r.Name); err != nil {
		return err
	}
	if err := d.Set("region_id", r.RegionID); err != nil {
		return err
	}
	if err := d.Set("pg_version", r.PgVersion); err != nil {
		return err
	}
	if err := d.Set("history_retention_seconds", r.HistoryRetentionSeconds); err != nil {
		return err
	}
	if err := d.Set("compute_provisioner", string(r.Provisioner)); err != nil {
		return err
	}
	if err := types.SetTristateBool(d, "store_password", &r.StorePasswords); err != nil {
		return err
	}

	if r.DefaultEndpointSettings != nil {
		if r.DefaultEndpointSettings.AutoscalingLimitMinCu != nil {
			err := d.Set("autoscaling_limit_min_cu", float64(*r.DefaultEndpointSettings.AutoscalingLimitMinCu))
			if err != nil {
				return err
			}
		}
		if r.DefaultEndpointSettings.AutoscalingLimitMaxCu != nil {
			err := d.Set("autoscaling_limit_max_cu", float64(*r.DefaultEndpointSettings.AutoscalingLimitMaxCu))
			if err != nil {
				return err
			}
		}
		if r.DefaultEndpointSettings.SuspendTimeoutSeconds != nil {
			err := d.Set("suspend_timeout_seconds", int(*r.DefaultEndpointSettings.SuspendTimeoutSeconds))
			if err != nil {
				return err
			}
		}
	}

	var primaryCompute = make(map[string]interface{}, len(schemaCompute.Elem.(*schemaSDK.Resource).Schema))
	if dbConnectionInfo.endpointID != "" {
		primaryCompute["id"] = dbConnectionInfo.endpointID
		primaryCompute["autoscaling_limit_min_cu"] = float64(dbConnectionInfo.defaultEndpoint.AutoscalingLimitMinCu)
		primaryCompute["autoscaling_limit_max_cu"] = float64(dbConnectionInfo.defaultEndpoint.AutoscalingLimitMaxCu)
		primaryCompute["suspend_timeout_seconds"] = int(dbConnectionInfo.defaultEndpoint.SuspendTimeoutSeconds)
		var name string
		if dbConnectionInfo.defaultEndpoint.Name != nil {
			name = *dbConnectionInfo.defaultEndpoint.Name
		}
		primaryCompute["name"] = name
		if err := d.Set("primary_compute", []interface{}{primaryCompute}); err != nil {
			return err
		}
	}

	if err := d.Set(
		"branch", []interface{}{
			map[string]interface{}{
				"id":            defaultBranchID,
				"name":          defaultBranchName,
				"role_name":     dbConnectionInfo.userName,
				"database_name": dbConnectionInfo.dbName,
			},
		},
	); err != nil {
		return err
	}

	_ = d.Set("quota", []interface{}{map[string]interface{}{}})

	if r.Settings != nil {
		if r.Settings.Quota != nil {
			if err := d.Set(
				"quota", []interface{}{
					map[string]interface{}{
						"active_time_seconds":  int(deref(r.Settings.Quota.ActiveTimeSeconds)),
						"compute_time_seconds": int(deref(r.Settings.Quota.ComputeTimeSeconds)),
						"written_data_bytes":   int(deref(r.Settings.Quota.WrittenDataBytes)),
						"data_transfer_bytes":  int(deref(r.Settings.Quota.DataTransferBytes)),
						"logical_size_bytes":   int(deref(r.Settings.Quota.LogicalSizeBytes)),
					},
				},
			); err != nil {
				return err
			}
		}

		var allowedIPs = make([]string, 0)
		var protectedBranchesOnly *bool
		if r.Settings.AllowedIps.Ips != nil {
			allowedIPs = r.Settings.AllowedIps.Ips
			protectedBranchesOnly = r.Settings.AllowedIps.ProtectedBranchesOnly
		}
		if err := d.Set("allowed_ips", allowedIPs); err != nil {
			return err
		}
		if _, ok := d.GetOk("allowed_ips_protected_branches_only"); ok ||
			(protectedBranchesOnly != nil && *protectedBranchesOnly) {
			if err := types.SetTristateBool(d, "allowed_ips_protected_branches_only", protectedBranchesOnly); err != nil {
				return err
			}
		}
		if _, ok := d.GetOk("block_public_connections"); ok ||
			(r.Settings.BlockPublicConnections != nil && *r.Settings.BlockPublicConnections) {
			if err := types.SetTristateBool(d, "block_public_connections", r.Settings.BlockPublicConnections); err != nil {
				return err
			}
		}
		if _, ok := d.GetOk("enable_logical_replication"); ok ||
			(r.Settings.EnableLogicalReplication != nil && *r.Settings.EnableLogicalReplication) {
			if err := types.SetTristateBool(d, "enable_logical_replication",
				r.Settings.EnableLogicalReplication); err != nil {
				return err
			}
		}

		if _, ok := d.GetOk("block_vpc_connections"); ok ||
			(r.Settings.BlockVpcConnections != nil && *r.Settings.BlockVpcConnections) {
			if err := types.SetTristateBool(d, "block_vpc_connections",
				r.Settings.BlockVpcConnections); err != nil {
				return err
			}
		}

		if r.Settings.Hipaa != nil {
			switch {
			case !*r.Settings.Hipaa && types.IsNull(d, "hipaa"):
			default:
				if err := types.SetTristateBool(d, "hipaa", r.Settings.Hipaa); err != nil {
					return err
				}
			}
		}
	}

	if err := d.Set("default_branch_id", defaultBranchID); err != nil {
		return err
	}

	if err := d.Set("database_host", dbConnectionInfo.host); err != nil {
		return err
	}

	if err := d.Set("database_host_pooler", dbConnectionInfo.poolerHost); err != nil {
		return err
	}

	if err := d.Set("database_name", dbConnectionInfo.dbName); err != nil {
		return err
	}

	if err := d.Set("database_user", dbConnectionInfo.userName); err != nil {
		return err
	}

	if err := d.Set("database_password", dbConnectionInfo.pass); err != nil {
		return err
	}

	if err := d.Set("connection_uri", dbConnectionInfo.connectionURI()); err != nil {
		return err
	}

	if err := d.Set("connection_uri_pooler", dbConnectionInfo.poolerConnectionURI()); err != nil {
		return err
	}

	if err := d.Set("default_endpoint_id", dbConnectionInfo.endpointID); err != nil {
		return err
	}

	if r.Settings != nil && r.Settings.MaintenanceWindow != nil {
		maintenanceWindow := r.Settings.MaintenanceWindow
		if err := d.Set("maintenance_window", []map[string]interface{}{
			{
				"weekdays":   maintenanceWindow.Weekdays,
				"start_time": maintenanceWindow.StartTime,
				"end_time":   maintenanceWindow.EndTime,
			},
		}); err != nil {
			return err
		}
	}

	if err := d.Set("default_branch_protected", defaultBranchProtected); err != nil {
		return err
	}

	return nil
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func resourceProjectDeleteRetry(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) diagSDK.Diagnostics {
	return projectReadiness.RetryWithFallback(resourceProjectDelete, ctx, d, meta, map[int]FallbackFn{
		http.StatusNotFound: func(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
			d.SetId("")
			return nil
		},
		http.StatusUnprocessableEntity: func(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
			d.SetId("")
			return nil
		},
	})
}

func resourceProjectUpdateRetry(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) diagSDK.Diagnostics {
	return projectReadiness.Retry(resourceProjectUpdate, ctx, d, meta)
}

func resourceProjectCreate(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "created Project")

	var orgID *string
	if v, ok := d.GetOk("org_id"); ok && v != "" {
		orgID = pointer(v.(string))
	}

	projectDef := neon.ProjectCreateRequestProject{
		OrgID:          orgID,
		Name:           pointer(d.Get("name").(string)),
		Provisioner:    pointer(neon.Provisioner(d.Get("compute_provisioner").(string))),
		RegionID:       pointer(d.Get("region_id").(string)),
		StorePasswords: types.GetTristateBool(d, "store_password"),
	}

	if v, ok := d.Get("history_retention_seconds").(int); ok && v >= 0 {
		projectDef.HistoryRetentionSeconds = pointer(int32(v))
	}

	if v, ok := d.GetOk("pg_version"); ok && v.(int) > 0 {
		projectDef.PgVersion = pointer(neon.PgVersion(v.(int)))
	}

	if v, ok := d.GetOk("autoscaling_limit_min_cu"); ok && v.(float64) > 0 {
		if projectDef.DefaultEndpointSettings == nil {
			projectDef.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		projectDef.DefaultEndpointSettings.AutoscalingLimitMinCu = pointer(neon.ComputeUnit(v.(float64)))
	}

	if v, ok := d.GetOk("autoscaling_limit_max_cu"); ok && v.(float64) > 0 {
		if projectDef.DefaultEndpointSettings == nil {
			projectDef.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		projectDef.DefaultEndpointSettings.AutoscalingLimitMaxCu = pointer(neon.ComputeUnit(v.(float64)))
	}

	if v, ok := d.GetOk("suspend_timeout_seconds"); ok && v.(int) > -2 {
		if projectDef.DefaultEndpointSettings == nil {
			projectDef.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		projectDef.DefaultEndpointSettings.SuspendTimeoutSeconds = pointer(neon.SuspendTimeoutSeconds(v.(int)))
	}

	if v, ok := d.GetOk("quota"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
			projectDef.Settings = &neon.ProjectSettingsData{
				Quota: mapToQuotaSettings(v),
			}
		}
	}

	if v, ok := d.GetOk("allowed_ips"); ok && len(v.([]interface{})) > 0 {
		var ips = make([]string, len(v.([]interface{})))
		for i, vv := range v.([]interface{}) {
			ips[i] = fmt.Sprintf("%v", vv)
		}

		if projectDef.Settings == nil {
			projectDef.Settings = &neon.ProjectSettingsData{}
		}
		projectDef.Settings.AllowedIps = &neon.AllowedIps{
			Ips: ips,
		}

		projectDef.Settings.AllowedIps.ProtectedBranchesOnly = types.GetTristateBool(d,
			"allowed_ips_protected_branches_only")
	}

	if v := types.GetTristateBool(d, "block_public_connections"); v != nil {
		if projectDef.Settings == nil {
			projectDef.Settings = &neon.ProjectSettingsData{}
		}
		projectDef.Settings.BlockPublicConnections = v
	}

	if v := types.GetTristateBool(d, "block_vpc_connections"); v != nil {
		if projectDef.Settings == nil {
			projectDef.Settings = &neon.ProjectSettingsData{}
		}
		projectDef.Settings.BlockVpcConnections = v
	}

	if v := types.GetTristateBool(d, "enable_logical_replication"); v != nil {
		if projectDef.Settings == nil {
			projectDef.Settings = &neon.ProjectSettingsData{}
		}
		projectDef.Settings.EnableLogicalReplication = v
	}

	if v := types.GetTristateBool(d, "hipaa"); v != nil {
		if projectDef.Settings == nil {
			projectDef.Settings = &neon.ProjectSettingsData{}
		}
		projectDef.Settings.Hipaa = v
	}

	if v, ok := d.GetOk("branch"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
			projectDef.Branch = mapToBranchSettings(v)
		}
	}

	if v, ok := d.GetOk("maintenance_window"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
			if projectDef.Settings == nil {
				projectDef.Settings = &neon.ProjectSettingsData{}
			}
			weekdaysRaw := v["weekdays"].([]interface{})
			var weekdays = make([]int, len(weekdaysRaw))
			for i, weekday := range weekdaysRaw {
				weekdays[i] = weekday.(int)
			}
			projectDef.Settings.MaintenanceWindow = &neon.MaintenanceWindow{
				EndTime:   v["end_time"].(string),
				StartTime: v["start_time"].(string),
				Weekdays:  weekdays,
			}
		}
	}

	client := meta.(sdkProject)

	resp, err := client.CreateProject(
		neon.ProjectCreateRequest{
			Project: projectDef,
		},
	)

	if err != nil {
		return err
	}

	waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)

	projectID := resp.ProjectResponse.Project.ID
	d.SetId(projectID)

	branch := resp.BranchResponse.Branch
	var info dbConnectionInfo
	info, err = newDbConnectionInfo(client, projectID, branch.ID, resp.EndpointsResponse.Endpoints,
		resp.DatabasesResponse.Databases)
	if err != nil {
		return err
	}

	defaultBranchProtected := d.Get("default_branch_protected").(bool)
	// Warning: The condition shall be updated if Neon changes the default behaviour.
	if defaultBranchProtected != branch.Protected {
		resp, err := client.UpdateProjectBranch(projectID, branch.ID, neon.BranchUpdateRequest{
			Branch: neon.BranchUpdateRequestBranch{
				Protected: &defaultBranchProtected,
			},
		})
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)
	}

	if v, ok := d.GetOk("primary_compute"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
			cfg := neon.EndpointUpdateRequestEndpoint{}
			if v, ok := v["autoscaling_limit_min_cu"].(float64); ok && v > 0 {
				cfg.AutoscalingLimitMinCu = pointer(neon.ComputeUnit(v))
			}

			if v, ok := v["autoscaling_limit_max_cu"].(float64); ok && v > 0 {
				cfg.AutoscalingLimitMaxCu = pointer(neon.ComputeUnit(v))
			}

			if v, ok := v["suspend_timeout_seconds"].(int); ok && v > -2 {
				cfg.SuspendTimeoutSeconds = pointer(neon.SuspendTimeoutSeconds(v))
			}

			if v, ok := v["name"].(string); ok && v != "" {
				cfg.Name = &v
			}

			client := meta.(*neon.Client)
			resp, err := client.UpdateProjectEndpoint(
				projectID,
				info.endpointID,
				neon.EndpointUpdateRequest{Endpoint: cfg},
			)
			if err != nil {
				return err
			}
			waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)
			info.defaultEndpoint = resp.EndpointResponse.Endpoint
		}
	}

	return updateStateProject(d, resp.ProjectResponse.Project, branch.ID, branch.Name, info, defaultBranchProtected)
}

func resourceProjectCreateRetry(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) diagSDK.Diagnostics {
	return projectReadiness.Retry(resourceProjectCreate, ctx, d, meta)
}

func resourceProjectUpdate(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "update Project")

	req := neon.ProjectUpdateRequest{
		Project: neon.ProjectUpdateRequestProject{},
	}

	if d.HasChange("name") {
		req.Project.Name = pointer(d.Get("name").(string))
	}

	if d.HasChange("history_retention_seconds") {
		req.Project.HistoryRetentionSeconds = pointer(int32(d.Get("history_retention_seconds").(int)))
	}

	if d.HasChange("autoscaling_limit_min_cu") {
		if v, ok := d.GetOk("autoscaling_limit_min_cu"); ok && v.(float64) > 0 {
			if req.Project.DefaultEndpointSettings == nil {
				req.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
			}
			req.Project.DefaultEndpointSettings.AutoscalingLimitMinCu = pointer(neon.ComputeUnit(v.(float64)))
		}
	}

	if d.HasChange("autoscaling_limit_max_cu") {
		if v, ok := d.GetOk("autoscaling_limit_max_cu"); ok && v.(float64) > 0 {
			if req.Project.DefaultEndpointSettings == nil {
				req.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
			}
			req.Project.DefaultEndpointSettings.AutoscalingLimitMaxCu = pointer(neon.ComputeUnit(v.(float64)))
		}
	}

	if d.HasChange("suspend_timeout_seconds") {
		if v, ok := d.GetOk("suspend_timeout_seconds"); ok && v.(int) > -2 {
			if req.Project.DefaultEndpointSettings == nil {
				req.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
			}
			req.Project.DefaultEndpointSettings.SuspendTimeoutSeconds = pointer(neon.SuspendTimeoutSeconds(v.(int)))
		}
	}

	var quota *neon.ProjectQuota
	if d.HasChange("quota") {
		if v, ok := d.GetOk("quota"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
			if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
				quota = mapToQuotaSettings(v)
			}
		}
	}

	var maintenanceWindow = new(neon.MaintenanceWindow) // nolint:ineffassign
	maintenanceWindow = nil                             // nolint:ineffassign
	if d.HasChange("maintenance_window") {
		if v, ok := d.GetOk("maintenance_window"); ok {
			if len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
				if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
					weekdaysRaw := v["weekdays"].([]interface{})
					var weekdays = make([]int, len(weekdaysRaw))
					for i, weekday := range weekdaysRaw {
						weekdays[i] = weekday.(int)
					}
					maintenanceWindow.Weekdays = weekdays
					maintenanceWindow.StartTime = v["start_time"].(string)
					maintenanceWindow.EndTime = v["end_time"].(string)
				}
			}
		}
	}

	withLogicalReplication := types.GetTristateBool(d, "enable_logical_replication")
	if _, ok := d.GetOk("enable_logical_replication"); !ok && d.HasChange("enable_logical_replication") {
		withLogicalReplication = pointer(false)
	}
	req.Project.Settings = &neon.ProjectSettingsData{
		Quota: quota,
		AllowedIps: &neon.AllowedIps{
			ProtectedBranchesOnly: types.GetTristateBool(d, "allowed_ips_protected_branches_only"),
		},
		BlockPublicConnections:   types.GetTristateBool(d, "block_public_connections"),
		EnableLogicalReplication: withLogicalReplication,
		MaintenanceWindow:        maintenanceWindow,
		BlockVpcConnections:      types.GetTristateBool(d, "block_vpc_connections"),
		Hipaa:                    types.GetTristateBool(d, "hipaa"),
	}

	if v, ok := d.GetOk("allowed_ips"); ok && len(v.([]interface{})) > 0 {
		var allowedIPs = make([]string, len(v.([]interface{})))
		for i, vv := range v.([]interface{}) {
			allowedIPs[i] = fmt.Sprintf("%v", vv)
		}
		req.Project.Settings.AllowedIps.Ips = allowedIPs
	}

	client := meta.(sdkProject)
	resp, err := client.UpdateProject(d.Id(), req)
	if err != nil {
		return err
	}

	waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)

	if d.HasChange("default_branch_protected") {
		defaultBranchProtected := d.Get("default_branch_protected").(bool)
		branchID := d.Get("default_branch_id").(string)
		resp, err := client.UpdateProjectBranch(d.Id(), branchID, neon.BranchUpdateRequest{
			Branch: neon.BranchUpdateRequestBranch{
				Protected: &defaultBranchProtected,
			},
		})
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)
	}

	if d.HasChange("primary_compute") {
		if v, ok := d.GetOk("primary_compute"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
			if v, ok := v.([]interface{})[0].(map[string]interface{}); ok && len(v) > 0 {
				cfg := neon.EndpointUpdateRequestEndpoint{}
				if v, ok := v["autoscaling_limit_min_cu"].(float64); ok && v > 0 {
					cfg.AutoscalingLimitMinCu = pointer(neon.ComputeUnit(v))
				}

				if v, ok := v["autoscaling_limit_max_cu"].(float64); ok && v > 0 {
					cfg.AutoscalingLimitMaxCu = pointer(neon.ComputeUnit(v))
				}

				if v, ok := v["suspend_timeout_seconds"].(int); ok && v > -2 {
					cfg.SuspendTimeoutSeconds = pointer(neon.SuspendTimeoutSeconds(v))
				}
				if v, ok := v["name"].(string); ok && v != "" {
					cfg.Name = &v
				}

				client := meta.(*neon.Client)
				resp, err := client.UpdateProjectEndpoint(
					d.Id(),
					v["id"].(string),
					neon.EndpointUpdateRequest{Endpoint: cfg},
				)
				if err != nil {
					return err
				}
				waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)
			}
		}
	}

	return resourceProjectRead(ctx, d, meta)
}

func resourceProjectRead(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "get Project")
	client := meta.(sdkProject)

	resp, err := client.GetProject(d.Id())
	if err != nil {
		return err
	}

	project := resp.Project

	branches, err := client.ListProjectBranches(d.Id(), nil, nil, nil, nil, nil, nil)
	if err != nil {
		return err
	}

	var branchMain neon.Branch
	for _, v := range branches.Branches {
		if v.Default {
			branchMain = v
			break
		}
	}

	if branchMain.ID == "" {
		return updateStateProject(d, project, branchMain.ID, branchMain.Name, dbConnectionInfo{}, branchMain.Protected)
	}

	endpoints, err := client.ListProjectBranchEndpoints(d.Id(), branchMain.ID)
	if err != nil {
		return err
	}

	dbs, err := client.ListProjectBranchDatabases(d.Id(), branchMain.ID)
	if err != nil {
		return err
	}

	info, err := newDbConnectionInfo(client, project.ID, branchMain.ID, endpoints.Endpoints, dbs.Databases)
	if err != nil {
		return err
	}

	return updateStateProject(d, project, branchMain.ID, branchMain.Name, info, branchMain.Protected)
}

func resourceProjectReadRetry(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) diagSDK.Diagnostics {
	return projectReadiness.RetryWithFallback(resourceProjectRead, ctx, d, meta, map[int]FallbackFn{
		http.StatusNotFound: func(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
			tflog.Debug(ctx, "project not found, deleting from the state",
				map[string]interface{}{"id": d.Id()})
			d.SetId("")
			return nil
		}})
}

func resourceProjectDelete(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "delete Project")

	if _, err := meta.(sdkProject).DeleteProject(d.Id()); err != nil {
		return err
	}

	d.SetId("")
	return updateStateProject(d, neon.Project{}, "", "", dbConnectionInfo{},
		false)
}

func resourceProjectImport(ctx context.Context, d *schemaSDK.ResourceData, meta interface{}) (
	[]*schemaSDK.ResourceData, error,
) {
	if diags := projectReadiness.Retry(resourceProjectRead, ctx, d, meta); diags.HasError() {
		return nil, errors.New(diags[0].Summary)
	}
	return []*schemaSDK.ResourceData{d}, nil
}

type sdkProject interface {
	GetProjectBranchRolePassword(string, string, string) (neon.RolePasswordResponse, error)
	CreateProject(neon.ProjectCreateRequest) (neon.CreatedProject, error)
	UpdateProject(string, neon.ProjectUpdateRequest) (neon.UpdateProjectRespObj, error)
	GetProject(string) (neon.ProjectResponse, error)
	ListProjectBranches(string, *string, *neon.ListProjectBranchesSortBy, *string, *neon.SortOrderParam, *uint16,
		*bool) (neon.ListProjectBranchesRespObj, error)
	ListProjectBranchEndpoints(string, string) (neon.EndpointsResponse, error)
	DeleteProject(string) (neon.ProjectResponse, error)
	ListProjectBranchDatabases(string, string) (neon.DatabasesResponse, error)
	GrantPermissionToProject(projectID string, cfg neon.GrantPermissionToProjectRequest) (neon.ProjectPermission, error)
	RevokePermissionFromProject(projectID string, permissionID string) (neon.ProjectPermission, error)
	ListProjectPermissions(projectID string) (neon.ProjectPermissions, error)
	UpdateProjectBranch(projectID string, branchID string, cfg neon.BranchUpdateRequest) (neon.BranchOperations, error)
	opsReader
}

var _ resource.ResourceWithConfigure = (*neonProjectResource)(nil)
var _ resource.ResourceWithImportState = (*neonProjectResource)(nil)

type neonProjectResourceModel struct {
	ID                              types.String                `tfsdk:"id"`
	OrgID                           types.String                `tfsdk:"org_id"`
	Name                            types.String                `tfsdk:"name"`
	RegionID                        types.String                `tfsdk:"region_id"`
	PgVersion                       types.Int64                 `tfsdk:"pg_version"`
	StorePassword                   types.Bool                  `tfsdk:"store_password"`
	HistoryRetentionSeconds         types.Int64                 `tfsdk:"history_retention_seconds"`
	AllowedIPs                      types.List                  `tfsdk:"allowed_ips"`
	AllowedIPsProtectedBranchesOnly types.Bool                  `tfsdk:"allowed_ips_protected_branches_only"`
	AutoscalingLimitMaxCU           types.Float64               `tfsdk:"autoscaling_limit_max_cu"`
	AutoscalingLimitMinCU           types.Float64               `tfsdk:"autoscaling_limit_min_cu"`
	BlockPublicConnections          types.Bool                  `tfsdk:"block_public_connections"`
	BlockVPCConnections             types.Bool                  `tfsdk:"block_vpc_connections"`
	ComputeProvisioner              types.String                `tfsdk:"compute_provisioner"`
	DatabaseHost                    types.String                `tfsdk:"database_host"`
	DatabaseHostPooler              types.String                `tfsdk:"database_host_pooler"`
	DatabaseName                    types.String                `tfsdk:"database_name"`
	DatabaseUser                    types.String                `tfsdk:"database_user"`
	DatabasePassword                types.String                `tfsdk:"database_password"`
	ConnectionURI                   types.String                `tfsdk:"connection_uri"`
	ConnectionURIPooler             types.String                `tfsdk:"connection_uri_pooler"`
	DefaultEndpointID               types.String                `tfsdk:"default_endpoint_id"`
	MaintenanceWindow               []maintenanceWindow         `tfsdk:"maintenance_window"`
	PrimaryCompute                  types.List                  `tfsdk:"primary_compute"`
	SuspendTimeoutSeconds           types.Int64                 `tfsdk:"suspend_timeout_seconds"`
	EnableLogicalReplication        types.Bool                  `tfsdk:"enable_logical_replication"`
	Hipaa                           types.Bool                  `tfsdk:"hipaa"`
	Quota                           projectQuota                `tfsdk:"quota"`
	Branch                          defaultProjectBranchModel   `tfsdk:"branch"`
	Endpoint                        defaultProjectEndpointModel `tfsdk:"endpoint"`

	// TODO: delete the release after the off. release
	DefaultBranchID types.String `tfsdk:"default_branch_id"`
	// TODO: delete the release after the off. release
	DefaultBranchProtected types.Bool `tfsdk:"default_branch_protected"`
}

type maintenanceWindow struct {
	Weekdays  types.List   `tfsdk:"weekdays"`
	StartTime types.String `tfsdk:"start_time"`
	EndTime   types.String `tfsdk:"end_time"`
}

type projectQuota struct {
	ActiveTimeSeconds  types.Int64 `tfsdk:"active_time_seconds"`
	ComputeTimeSeconds types.Int64 `tfsdk:"compute_time_seconds"`
	WrittenDataBytes   types.Int64 `tfsdk:"written_data_bytes"`
	DataTransferBytes  types.Int64 `tfsdk:"data_transfer_bytes"`
	LogicalSizeBytes   types.Int64 `tfsdk:"logical_size_bytes"`
}

type defaultProjectBranchModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	RoleName     types.String `tfsdk:"role_name"`
	DatabaseName types.String `tfsdk:"database_name"`
	Protected    types.Bool   `tfsdk:"protected"`
}

type defaultProjectEndpointModel struct {
	ID                          types.String  `tfsdk:"id"`
	Name                        types.String  `tfsdk:"name"`
	AutoscalingLimitMinCU       types.Float64 `tfsdk:"autoscaling_limit_min_cu"`
	AutoscalingLimitMaxCU       types.Float64 `tfsdk:"autoscaling_limit_max_cu"`
	SuspendTimeoutSeconds       types.Int64   `tfsdk:"suspend_timeout_seconds"`
	ComputeProvisioner          types.String  `tfsdk:"compute_provisioner"`
	Disabled                    types.Bool    `tfsdk:"disabled"`
	EnabledLibraries            types.List    `tfsdk:"enabled_libraries"`
	EnabledLibrariesUseDefaults types.Bool    `tfsdk:"enabled_libraries_use_defaults"`
	PasswordlessAccess          types.Bool    `tfsdk:"passwordless_access"`
	Host                        types.String  `tfsdk:"host"`
	HostPooler                  types.String  `tfsdk:"host_pooler"`
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

func (r neonProjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresStringReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	endpointAttributesSchema := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "Endpoint ID.",
			Computed:    true,
		},
	}
	maps.Copy(endpointAttributesSchema, endpointConfigAttributesSchema)

	resp.Schema = schema.Schema{
		Description: `Neon Project.

Note that this resource manages five resources:

- Project: https://neon.com/docs/reference/glossary#project
- Root branch: https://neon.com/docs/reference/glossary#root-branch
- Primary compute with the default read-write endpoint: https://neon.com/docs/reference/glossary#compute
- Default database: https://neon.com/docs/reference/glossary#database
- Default role: https://neon.com/docs/reference/glossary#postgres-role
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Project ID.",
				Computed:    true,
			},
			"org_id": schema.StringAttribute{
				Description:   "Identifier of the organisation to which this project belongs.",
				Computed:      true,
				Optional:      true,
				PlanModifiers: requiresStringReplace,
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
				PlanModifiers: requiresStringReplace,
			},
			"pg_version": schema.Int64Attribute{
				Description: "Postgres version",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"store_password": schema.BoolAttribute{
				Description: "Whether or not passwords are stored for roles in the Neon project.\nStoring passwords facilitates access to Neon features that require authorization.",
				Optional:    true,
				Default:     booldefault.StaticBool(true),
			},
			"history_retention_seconds": schema.Int64Attribute{
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
			"default_branch_id": schema.StringAttribute{
				Description: "Default branch ID.",
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
			"default_endpoint_id": schema.StringAttribute{
				Description: "Default endpoint ID.",
				Computed:    true,
			},
			"default_branch_protected": schema.BoolAttribute{
				Description:        "Set default branch as protected. **Note** that the default value is false.",
				DeprecationMessage: "Use the `protected` attribute in the block `branch` instead.",
				Optional:           true,
				Default:            booldefault.StaticBool(false),
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
			"database_password": schema.StringAttribute{
				Description:        "The password for the default database user.",
				DeprecationMessage: "The attribute is always empty, use the ephemeral resource `neon_role` instead.",
				Computed:           true,
				Sensitive:          true,
				Default:            stringdefault.StaticString(""),
			},
			"database_host": schema.StringAttribute{
				Description: "The hostname of the default compute endpoint.",
				Computed:    true,
			},
			"database_host_pooler": schema.StringAttribute{
				Description: "The hostname of the default compute endpoint accessible through the connection pool (pgbouncer).",
				Computed:    true,
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
			"hipaa": schema.BoolAttribute{
				Description:   "Enable HIPAA compliance for the project.\nNote that HIPAA must be configured for the organization first.\n\n**Warning**: Once enabled, HIPAA cannot be disabled.",
				Computed:      true,
				Optional:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
		},
		Blocks: map[string]schema.Block{
			"branch": schema.SingleNestedBlock{
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{
						Description: "Branch ID.",
						Computed:    true,
					},
					"name": schema.StringAttribute{
						Description: "The name of the default branch provisioned upon creation of new project.\nIf not specified, the default branch name will be used.",
						Optional:    true,
						Computed:    true,
					},
					"protected": schema.BoolAttribute{
						Description: "Set default branch as protected. **Note** that the default value is false.",
						Optional:    true,
						Default:     booldefault.StaticBool(false),
					},
				},
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
			"maintenance_window": schema.ListNestedBlock{
				Description: "A time period during which Neon may perform maintenance on the project's infrastructure. During this time, the project's compute endpoints may be unavailable and existing connections can be interrupted.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"weekdays": schema.ListAttribute{
							Description: "A list of weekdays when the maintenance window is active. Encoded as ints, where 1 - Monday, and 7 - Sunday.",
							Required:    true,
							ElementType: types.Int64Type,
							Validators:  []validator.List{validatorMaintenanceWeekdays{}},
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
			"primary_compute": schema.SingleNestedBlock{
				Description: "Primary read-write compute resource provisioned with the project.",
				Attributes:  endpointAttributesSchema,
			},
		},
	}
}

type validatorMaintenanceWeekdays struct{}

func (v validatorMaintenanceWeekdays) Description(_ context.Context) string {
	return "Validator of the maintenance window's weekdays."
}

func (v validatorMaintenanceWeekdays) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validatorMaintenanceWeekdays) ValidateList(ctx context.Context, req validator.ListRequest,
	resp *validator.ListResponse) {
	var data neonProjectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(data.MaintenanceWindow) == 0 {
		return
	}

	for i, window := range data.MaintenanceWindow {
		errMsg := fmt.Sprintf("undefined maintenance window %d", i)
		switch {
		case window.Weekdays.IsUnknown() || window.Weekdays.IsNull():
			resp.Diagnostics.AddError(errMsg, "unknown weekdays")
		case len(window.Weekdays.Elements()) > 7 || len(window.Weekdays.Elements()) < 1:
			resp.Diagnostics.AddError(errMsg, "invalid number of weekdays, it must be between 1 and 7")
		default:
			for j, weekday := range window.Weekdays.Elements() {
				if weekday.IsNull() || weekday.IsUnknown() {
					resp.Diagnostics.AddError(errMsg, fmt.Sprintf("unknown weekday %d", j))
				} else {
					switch weekday.String() {
					case "1", "2", "3", "4", "5", "6", "7":
					default:
						resp.Diagnostics.AddError(errMsg,
							fmt.Sprintf("invalid weekday %d, the value must be from 1 (Mon.) to 7 (Sun.)", j))
					}
				}
			}
		}
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

	// TODO implement me
	panic("implement me")
}

func (r neonProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// TODO implement me
	panic("implement me")
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
