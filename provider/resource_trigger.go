package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.ResourceWithConfigure   = (*neonTriggerResource)(nil)
	_ resource.ResourceWithImportState = (*neonTriggerResource)(nil)
)

type neonTriggerResource struct {
	client *neon.Client
}

type neonTriggerResourceModel struct {
	ID                   types.String               `tfsdk:"id"`
	ProjectID            types.String               `tfsdk:"project_id"`
	BranchID             types.String               `tfsdk:"branch_id"`
	Name                 types.String               `tfsdk:"name"`
	Type                 types.String               `tfsdk:"type"`
	FunctionSlug         types.String               `tfsdk:"function_slug"`
	FunctionPath         types.String               `tfsdk:"function_path"`
	Enabled              types.Bool                 `tfsdk:"enabled"`
	Schedule             *scheduleModel             `tfsdk:"schedule"`
	StorageObjectCreated *storageObjectCreatedModel `tfsdk:"storage_object_created"`
	TriggerID            types.String               `tfsdk:"trigger_id"`
	Version              types.Int64                `tfsdk:"version"`
	SourceBranchID       types.String               `tfsdk:"source_branch_id"`
	NextRunAt            types.String               `tfsdk:"next_run_at"`
	Inherited            types.Bool                 `tfsdk:"inherited"`
}

type scheduleModel struct {
	Cron types.String `tfsdk:"cron"`
}

type storageObjectCreatedModel struct {
	BucketName types.String `tfsdk:"bucket_name"`
	Prefix     types.String `tfsdk:"prefix"`
}

func NewNeonTriggerResource() resource.Resource {
	return &neonTriggerResource{}
}

func (r *neonTriggerResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_trigger"
}

func (r *neonTriggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Neon trigger (Function invocation on a schedule or after a branch-bucket object upload).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite ID of the form <project_id>/<branch_id>/<trigger_id>.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "The Neon branch ID.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable trigger name. Unique among triggers visible on the branch.",
			},
			"type": schema.StringAttribute{
				Required:    true,
				Validators:  []validator.String{triggerTypeValidator{}},
				Description: "Trigger type discriminator. One of `schedule` or `storage_object_created`.",
			},
			"function_slug": schema.StringAttribute{
				Required:    true,
				Description: "The branch-local Function slug resolved when an occurrence is consumed.",
			},
			"function_path": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Path passed to the target Function. Defaults to `/`.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether future occurrences should fire the trigger.",
			},
			"schedule": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"cron": schema.StringAttribute{
						Required:    true,
						Description: "Numeric five-field cron expression (minute through day-of-week), interpreted in UTC.",
					},
				},
			},
			"storage_object_created": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"bucket_name": schema.StringAttribute{
						Required:    true,
						Description: "The exact object-storage bucket name to watch.",
					},
					"prefix": schema.StringAttribute{
						Optional:    true,
						Description: "Optional object-key prefix. Max 1024 UTF-8 bytes.",
					},
				},
			},
			"trigger_id": schema.StringAttribute{
				Computed:    true,
				Description: "Opaque, server-minted trigger ID.",
			},
			"version": schema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic configuration version.",
			},
			"source_branch_id": schema.StringAttribute{
				Computed:    true,
				Description: "Public branch_id of the branch that authored the effective configuration.",
			},
			"next_run_at": schema.StringAttribute{
				Computed:    true,
				Description: "Next scheduled occurrence as an RFC 3339 UTC timestamp. Null while disabled or inherited and not explicitly enabled on this branch.",
			},
			"inherited": schema.BoolAttribute{
				Computed:    true,
				Description: "True when the effective configuration was authored on an ancestor branch.",
			},
		},
	}
}

func (r *neonTriggerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var planned neonTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() {
		return
	}

	typ := planned.Type.ValueString()
	scheduleSet := planned.Schedule != nil
	storageSet := planned.StorageObjectCreated != nil

	switch typ {
	case "schedule":
		if !scheduleSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("schedule"),
				"Missing required block",
				"When type is `schedule`, the `schedule` block is required.",
			)
		}
		if storageSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("storage_object_created"),
				"Incompatible block for type",
				"When type is `schedule`, the `storage_object_created` block must not be set.",
			)
		}
	case "storage_object_created":
		if !storageSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("storage_object_created"),
				"Missing required block",
				"When type is `storage_object_created`, the `storage_object_created` block is required.",
			)
		}
		if scheduleSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("schedule"),
				"Incompatible block for type",
				"When type is `storage_object_created`, the `schedule` block must not be set.",
			)
		}
	}
}

func (r *neonTriggerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*neon.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			"Expected *neon.Client, got an unexpected type.",
		)
		return
	}
	r.client = client
}

func (r *neonTriggerResource) ImportState(_ context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"neon_trigger does not support import",
		"neon_trigger is provisioned via `terraform apply` only; the server mints `trigger_id` on create. Re-author the trigger in your configuration to manage it with Terraform.",
	)
}

func (r *neonTriggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := buildTriggerCreateRequest(&plan)

	var result neon.TriggerResponse
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		result, err = r.client.CreateProjectBranchTrigger(
			plan.ProjectID.ValueString(),
			plan.BranchID.ValueString(),
			cfg,
		)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonTriggerModel(&plan, result.Trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonTriggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.TriggersListResponse
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(_ context.Context) error {
		var err error
		result, err = r.client.ListProjectBranchTriggers(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
		)
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			resp.State.RemoveResource(ctx)
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	var metadata *neon.Trigger
	for i := range result.Triggers {
		if state.TriggerID.ValueString() == triggerIDFromTrigger(result.Triggers[i]) {
			metadata = &result.Triggers[i]
			break
		}
	}
	if metadata == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	setNeonTriggerModel(&state, *metadata)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonTriggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := buildTriggerUpdateRequest(&plan)

	var result neon.TriggerResponse
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		var err error
		result, err = r.client.UpdateProjectBranchTrigger(
			plan.ProjectID.ValueString(),
			plan.BranchID.ValueString(),
			neon.TriggerID(plan.TriggerID.ValueString()),
			cfg,
		)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonTriggerModel(&plan, result.Trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonTriggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(_ context.Context) error {
		return r.client.DeleteProjectBranchTrigger(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			neon.TriggerID(state.TriggerID.ValueString()),
		)
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			return nil
		},
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func buildTriggerCreateRequest(plan *neonTriggerResourceModel) neon.TriggerCreateRequest {
	cfg := neon.TriggerCreateRequest{}

	typ := plan.Type.ValueString()
	switch typ {
	case "schedule":
		typeVal, _ := neon.NewScheduleTriggerCreateRequestType("schedule")
		cfg.ScheduleTriggerCreateRequest.Type = typeVal
		cfg.ScheduleTriggerCreateRequest.Name = plan.Name.ValueString()
		cfg.ScheduleTriggerCreateRequest.FunctionSlug = plan.FunctionSlug.ValueString()
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.ScheduleTriggerCreateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.ScheduleTriggerCreateRequest.Enabled = &en
		}
		if plan.Schedule != nil {
			cfg.ScheduleTriggerCreateRequest.Schedule = neon.FunctionTriggerSchedule{
				Cron: plan.Schedule.Cron.ValueString(),
			}
		}
	case "storage_object_created":
		typeVal, _ := neon.NewStorageObjectCreatedTriggerCreateRequestType("storage_object_created")
		cfg.StorageObjectCreatedTriggerCreateRequest.Type = typeVal
		cfg.StorageObjectCreatedTriggerCreateRequest.Name = plan.Name.ValueString()
		cfg.StorageObjectCreatedTriggerCreateRequest.FunctionSlug = plan.FunctionSlug.ValueString()
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.StorageObjectCreatedTriggerCreateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.StorageObjectCreatedTriggerCreateRequest.Enabled = &en
		}
		if plan.StorageObjectCreated != nil {
			cfg.StorageObjectCreatedTriggerCreateRequest.StorageObjectCreated = neon.FunctionTriggerStorageObjectCreated{
				BucketName: plan.StorageObjectCreated.BucketName.ValueString(),
			}
			if !plan.StorageObjectCreated.Prefix.IsNull() && !plan.StorageObjectCreated.Prefix.IsUnknown() {
				p := plan.StorageObjectCreated.Prefix.ValueString()
				cfg.StorageObjectCreatedTriggerCreateRequest.StorageObjectCreated.Prefix = &p
			}
		}
	}
	return cfg
}

func buildTriggerUpdateRequest(plan *neonTriggerResourceModel) neon.TriggerUpdateRequest {
	cfg := neon.TriggerUpdateRequest{}

	typ := plan.Type.ValueString()
	switch typ {
	case "schedule":
		typeVal, _ := neon.NewScheduleTriggerUpdateRequestType("schedule")
		cfg.ScheduleTriggerUpdateRequest.Type = typeVal
		name := plan.Name.ValueString()
		cfg.ScheduleTriggerUpdateRequest.Name = &name
		slug := plan.FunctionSlug.ValueString()
		cfg.ScheduleTriggerUpdateRequest.FunctionSlug = &slug
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.ScheduleTriggerUpdateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.ScheduleTriggerUpdateRequest.Enabled = &en
		}
		if plan.Schedule != nil {
			cron := plan.Schedule.Cron.ValueString()
			cfg.ScheduleTriggerUpdateRequest.Schedule = &neon.FunctionTriggerSchedule{Cron: cron}
		}
	case "storage_object_created":
		typeVal, _ := neon.NewStorageObjectCreatedTriggerUpdateRequestType("storage_object_created")
		cfg.StorageObjectCreatedTriggerUpdateRequest.Type = typeVal
		name := plan.Name.ValueString()
		cfg.StorageObjectCreatedTriggerUpdateRequest.Name = &name
		slug := plan.FunctionSlug.ValueString()
		cfg.StorageObjectCreatedTriggerUpdateRequest.FunctionSlug = &slug
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.StorageObjectCreatedTriggerUpdateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.StorageObjectCreatedTriggerUpdateRequest.Enabled = &en
		}
		if plan.StorageObjectCreated != nil {
			soc := neon.FunctionTriggerStorageObjectCreated{
				BucketName: plan.StorageObjectCreated.BucketName.ValueString(),
			}
			if !plan.StorageObjectCreated.Prefix.IsNull() && !plan.StorageObjectCreated.Prefix.IsUnknown() {
				p := plan.StorageObjectCreated.Prefix.ValueString()
				soc.Prefix = &p
			}
			cfg.StorageObjectCreatedTriggerUpdateRequest.StorageObjectCreated = &soc
		}
	}
	return cfg
}

func setNeonTriggerModel(model *neonTriggerResourceModel, t neon.Trigger) {
	triggerID := triggerIDFromTrigger(t)
	model.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
		model.ProjectID.ValueString(), model.BranchID.ValueString(), triggerID))
	model.TriggerID = types.StringValue(triggerID)

	scheduleType := t.ScheduleTrigger.Type.String()
	storageType := t.StorageObjectCreatedTrigger.Type.String()

	switch {
	case scheduleType == "schedule":
		setNeonTriggerModelFromSchedule(model, t.ScheduleTrigger)
	case storageType == "storage_object_created":
		setNeonTriggerModelFromStorage(model, t.StorageObjectCreatedTrigger)
	}
}

func triggerIDFromTrigger(t neon.Trigger) string {
	if t.ScheduleTrigger.Type.String() == "schedule" {
		return string(t.ScheduleTrigger.TriggerID)
	}
	return string(t.StorageObjectCreatedTrigger.TriggerID)
}

func setNeonTriggerModelFromSchedule(model *neonTriggerResourceModel, st neon.ScheduleTrigger) {
	model.Type = types.StringValue("schedule")
	model.Name = types.StringValue(st.Name)
	model.FunctionSlug = types.StringValue(st.FunctionSlug)
	model.FunctionPath = types.StringValue(st.FunctionPath)
	model.Enabled = types.BoolValue(st.Enabled)
	model.Inherited = types.BoolValue(st.Inherited)
	model.Version = types.Int64Value(st.Version)
	model.SourceBranchID = types.StringValue(st.SourceBranchID)
	if st.NextRunAt != "" {
		model.NextRunAt = types.StringValue(st.NextRunAt)
	} else {
		model.NextRunAt = types.StringNull()
	}
	if st.Schedule.Cron != "" {
		model.Schedule = &scheduleModel{Cron: types.StringValue(st.Schedule.Cron)}
	} else {
		model.Schedule = nil
	}
	model.StorageObjectCreated = nil
}

func setNeonTriggerModelFromStorage(model *neonTriggerResourceModel, st neon.StorageObjectCreatedTrigger) {
	model.Type = types.StringValue("storage_object_created")
	model.Name = types.StringValue(st.Name)
	model.FunctionSlug = types.StringValue(st.FunctionSlug)
	model.FunctionPath = types.StringValue(st.FunctionPath)
	model.Enabled = types.BoolValue(st.Enabled)
	model.Inherited = types.BoolValue(st.Inherited)
	model.Version = types.Int64Value(st.Version)
	model.SourceBranchID = types.StringValue(st.SourceBranchID)
	model.NextRunAt = types.StringNull()
	soc := &storageObjectCreatedModel{
		BucketName: types.StringValue(st.StorageObjectCreated.BucketName),
	}
	if st.StorageObjectCreated.Prefix != nil {
		soc.Prefix = types.StringValue(*st.StorageObjectCreated.Prefix)
	} else {
		soc.Prefix = types.StringNull()
	}
	model.StorageObjectCreated = soc
	model.Schedule = nil
}

type triggerTypeValidator struct{}

func (triggerTypeValidator) Description(_ context.Context) string {
	return "validates that the trigger type is `schedule` or `storage_object_created`"
}

func (v triggerTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (triggerTypeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if _, err := neon.NewScheduleTriggerType(s); err == nil {
		return
	}
	if _, err := neon.NewStorageObjectCreatedTriggerType(s); err == nil {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid trigger type",
		fmt.Sprintf("%q is not a valid trigger type; expected `schedule` or `storage_object_created`.", s),
	)
}
