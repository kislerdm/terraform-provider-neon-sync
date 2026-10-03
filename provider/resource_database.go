package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	schemaTFSDK "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	typesTFSDK "github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"
)

func resourceDatabase() *schema.Resource {
	return &schema.Resource{
		Description:   `Project Database. See details: https://neon.tech/docs/manage/databases/`,
		SchemaVersion: 7,
		Importer: &schema.ResourceImporter{
			StateContext: resourceDatabaseImport,
		},
		CreateContext: resourceDatabaseCreateRetry,
		ReadContext:   resourceDatabaseReadRetry,
		UpdateContext: resourceDatabaseUpdateRetry,
		DeleteContext: resourceDatabaseDeleteRetry,
		Schema: map[string]*schema.Schema{
			"project_id": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Project ID.",
			},
			"branch_id": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Branch ID.",
			},
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Database name.",
			},
			"owner_name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Role name of the database owner.",
			},
		},
	}
}

func updateStateDatabase(d *schema.ResourceData, v neon.Database) error {
	if err := d.Set("owner_name", v.OwnerName); err != nil {
		return err
	}
	return nil
}

func resourceDatabaseCreateRetry(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return projectReadiness.Retry(resourceDatabaseCreate, ctx, d, meta)
}

func resourceDatabaseCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "created Database")

	r := complexID{
		ProjectID: d.Get("project_id").(string),
		BranchID:  d.Get("branch_id").(string),
		Name:      d.Get("name").(string),
	}
	client := meta.(*neon.Client)
	resp, err := client.CreateProjectBranchDatabase(
		r.ProjectID, r.BranchID, neon.DatabaseCreateRequest{
			Database: neon.DatabaseCreateRequestDatabase{
				Name:      r.Name,
				OwnerName: d.Get("owner_name").(string),
			},
		},
	)
	if err != nil {
		return err
	}
	waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)

	d.SetId(r.toString())

	return updateStateDatabase(d, resp.DatabaseResponse.Database)
}

func resourceDatabaseReadRetry(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return projectReadiness.RetryWithFallback(resourceDatabaseRead, ctx, d, meta, map[int]FallbackFn{
		http.StatusNotFound: func(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
			tflog.Debug(ctx, "database not found, removing from state",
				map[string]interface{}{
					"id":         d.Id(),
					"project_id": d.Get("project_id"),
					"branch_id":  d.Get("branch_id"),
				})
			d.SetId("")
			return nil
		}})
}

func resourceDatabaseRead(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "read Database")

	resp, err := meta.(*neon.Client).GetProjectBranchDatabase(
		d.Get("project_id").(string), d.Get("branch_id").(string), d.Get("name").(string),
	)
	if err != nil {
		return err
	}

	return updateStateDatabase(d, resp.Database)
}

func resourceDatabaseUpdateRetry(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return projectReadiness.Retry(resourceDatabaseUpdate, ctx, d, meta)
}

func resourceDatabaseUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "update Database")

	r, err := parseComplexID(d.Id())
	if err != nil {
		panic(err)
	}

	client := meta.(*neon.Client)
	resp, err := client.UpdateProjectBranchDatabase(
		r.ProjectID, r.BranchID, r.Name,
		neon.DatabaseUpdateRequest{
			Database: neon.DatabaseUpdateRequestDatabase{
				Name:      pointer(d.Get("name").(string)),
				OwnerName: pointer(d.Get("owner_name").(string)),
			},
		},
	)
	if err != nil {
		return err
	}
	waitUnfinishedOperations(ctx, client, resp.OperationsResponse.Operations)
	r.Name = resp.DatabaseResponse.Database.Name
	d.SetId(r.toString())
	return updateStateDatabase(d, resp.Database)
}

func resourceDatabaseDeleteRetry(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return projectReadiness.RetryWithFallback(resourceDatabaseDelete, ctx, d, meta, map[int]FallbackFn{
		http.StatusNotFound: func(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
			d.SetId("")
			return nil
		},
		http.StatusUnprocessableEntity: func(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
			d.SetId("")
			return nil
		},
	})
}

func resourceDatabaseDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) error {
	tflog.Trace(ctx, "delete Database")
	client := meta.(*neon.Client)
	op, err := client.DeleteProjectBranchDatabase(
		d.Get("project_id").(string),
		d.Get("branch_id").(string),
		d.Get("name").(string),
	)
	if err != nil {
		return err
	}
	waitUnfinishedOperations(ctx, client, op.OperationsResponse.Operations)
	d.SetId("")
	return updateStateDatabase(d, neon.Database{})
}

func resourceDatabaseImport(ctx context.Context, d *schema.ResourceData, meta interface{}) (
	[]*schema.ResourceData, error,
) {
	tflog.Trace(ctx, "import Database")

	r, err := parseComplexID(d.Id())
	if err != nil {
		return nil, err
	}

	setResourceAttrsFromComplexID(d, r)

	if diags := projectReadiness.Retry(resourceDatabaseRead, ctx, d, meta); diags.HasError() {
		setResourceAttrsFromComplexID(d, complexID{})
		d.SetId("")
		return nil, errors.New(diags[0].Summary)
	}

	return []*schema.ResourceData{d}, nil
}

var _ resource.ResourceWithConfigure = (*neonDatabase)(nil)
var _ resource.ResourceWithImportState = (*neonDatabase)(nil)

type neonDatabase struct {
	client *neon.Client
}

type neonDatabaseResourceModel struct {
	ID        typesTFSDK.String `tfsdk:"id"`
	ProjectID typesTFSDK.String `tfsdk:"project_id"`
	BranchID  typesTFSDK.String `tfsdk:"branch_id"`
	Name      typesTFSDK.String `tfsdk:"name"`
	OwnerName typesTFSDK.String `tfsdk:"owner_name"`
}

func (m *neonDatabaseResourceModel) inferAttr(db neon.Database) {
	m.ID = typesTFSDK.StringValue(m.ProjectID.ValueString() + "/" + db.BranchID + "/" + db.Name)
	m.Name = typesTFSDK.StringValue(db.Name)
	m.OwnerName = typesTFSDK.StringValue(db.OwnerName)
}

func NewNeonDatabaseResource() resource.Resource {
	return &neonDatabase{}
}

func (r *neonDatabase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *providerAdapter, got an unexpected type.")
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	r.client = client.sdk
}

func (r neonDatabase) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_database"
}

func (r neonDatabase) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schemaTFSDK.Schema{
		Description: `Project Database. See details: https://neon.tech/docs/manage/databases/`,
		Attributes: map[string]schemaTFSDK.Attribute{
			"id": schemaTFSDK.StringAttribute{
				Computed:    true,
				Description: "The database ID.",
			},
			"project_id": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Project ID.",
			},
			"branch_id": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Branch ID.",
			},
			"name": schemaTFSDK.StringAttribute{
				Required:    true,
				Description: "Database name.",
			},
			"owner_name": schemaTFSDK.StringAttribute{
				Required:    true,
				Description: "Database owner name.",
			},
		},
	}
}

func (r neonDatabase) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.DatabaseCreateRequest{
		Database: neon.DatabaseCreateRequestDatabase{
			Name:      state.Name.ValueString(),
			OwnerName: state.OwnerName.ValueString(),
		},
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		re, err := r.client.CreateProjectBranchDatabase(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			cfg)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations)
		db = re.DatabaseResponse.Database
		return nil
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonDatabase) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		dbResp, err := r.client.GetProjectBranchDatabase(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Name.ValueString())
		db = dbResp.Database
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)

	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if db.BranchID == "" {
		resp.Diagnostics.AddWarning("database not found",
			fmt.Sprintf("database %q not found in the project/branch \"%s/%s\"", state.Name.ValueString(),
				state.ProjectID.ValueString(), state.BranchID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonDatabase) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.DatabaseUpdateRequest{}
	if !state.Name.Equal(plan.Name) {
		cfg.Database.Name = plan.Name.ValueStringPointer()
	}
	if !state.OwnerName.Equal(plan.OwnerName) {
		cfg.Database.OwnerName = plan.OwnerName.ValueStringPointer()
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		dbResp, err := r.client.UpdateProjectBranchDatabase(state.ProjectID.ValueString(),
			state.BranchID.ValueString(), state.Name.ValueString(), cfg)
		waitUnfinishedOperations(ctx, r.client, dbResp.OperationsResponse.Operations)
		db = dbResp.Database
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if db.BranchID == "" {
		resp.Diagnostics.AddWarning("database not found",
			fmt.Sprintf("database %q not found in the project/branch \"%s/%s\"", state.Name.ValueString(),
				state.ProjectID.ValueString(), state.BranchID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonDatabase) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		re, err := r.client.DeleteProjectBranchDatabase(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Name.ValueString())
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations)
		return nil
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)

	resp.State.RemoveResource(ctx)
}

func (r neonDatabase) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 3)
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State Database ID",
			"Expected an import ID in the form <project_id>/<neon_branch_id>/<database_name>.",
		)
		return
	}

	projectID := els[0]
	branchID := els[1]
	databaseName := els[2]
	if !isValidBranchID(branchID) {
		resp.Diagnostics.AddError("provided Neon Branch ID is not valid", "")
		return
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
		dbResp, err := r.client.GetProjectBranchDatabase(projectID, branchID, databaseName)
		db = dbResp.Database
		return err
	}, ctx)...)

	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if db.BranchID == "" {
		resp.Diagnostics.AddWarning("database not found",
			fmt.Sprintf("database %q not found in the project/branch \"%s/%s\"", databaseName, projectID, branchID))
		return
	}

	var state neonDatabaseResourceModel
	state.ProjectID = typesTFSDK.StringValue(projectID)
	state.BranchID = typesTFSDK.StringValue(branchID)
	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
