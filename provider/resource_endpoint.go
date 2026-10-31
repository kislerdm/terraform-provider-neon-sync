package provider

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	endpointTypeRW       = neon.EndpointTypeReadWrite
	endpointTypeReadOnly = neon.EndpointTypeReadOnly
)

var _ resource.ResourceWithConfigure = (*neonEndpointResource)(nil)
var _ resource.ResourceWithImportState = (*neonEndpointResource)(nil)

type neonEndpointResource struct {
	client *neon.Client
}

type neonEndpointResourceModel struct {
	ID                          types.String      `tfsdk:"id"`
	ProjectID                   types.String      `tfsdk:"project_id"`
	BranchID                    types.String      `tfsdk:"branch_id"`
	RegionID                    types.String      `tfsdk:"region_id"`
	Name                        types.String      `tfsdk:"name"`
	Type                        types.String      `tfsdk:"type"`
	AutoscalingLimitMinCU       types.Float64     `tfsdk:"autoscaling_limit_min_cu"`
	AutoscalingLimitMaxCU       types.Float64     `tfsdk:"autoscaling_limit_max_cu"`
	SuspendTimeoutSeconds       types.Int64       `tfsdk:"suspend_timeout_seconds"`
	ComputeProvisioner          types.String      `tfsdk:"compute_provisioner"`
	PgSettings                  map[string]string `tfsdk:"pg_settings"`
	Disabled                    types.Bool        `tfsdk:"disabled"`
	EnabledLibraries            []string          `tfsdk:"enabled_libraries"`
	EnabledLibrariesUseDefaults types.Bool        `tfsdk:"enabled_libraries_use_defaults"`
	PasswordlessAccess          types.Bool        `tfsdk:"passwordless_access"`
	Host                        types.String      `tfsdk:"host"`
	HostPooler                  types.String      `tfsdk:"host_pooler"`
}

func (v *neonEndpointResourceModel) inferAttr(endpoint neon.Endpoint) {
	v.ID = types.StringValue(endpoint.ID)
	v.ProjectID = types.StringValue(endpoint.ProjectID)
	v.BranchID = types.StringValue(endpoint.BranchID)
	v.RegionID = types.StringValue(endpoint.RegionID)
	v.Name = types.StringPointerValue(endpoint.Name)
	v.Type = types.StringValue(endpoint.Type.String())
	v.AutoscalingLimitMinCU = types.Float64Value(float64(endpoint.AutoscalingLimitMinCu))
	v.AutoscalingLimitMaxCU = types.Float64Value(float64(endpoint.AutoscalingLimitMaxCu))
	v.SuspendTimeoutSeconds = types.Int64Value(int64(endpoint.SuspendTimeoutSeconds))
	v.ComputeProvisioner = types.StringValue(string(endpoint.Provisioner))
	if endpoint.Settings.PgSettings != nil && len(*endpoint.Settings.PgSettings) > 0 {
		v.PgSettings = make(map[string]string, len(*endpoint.Settings.PgSettings))
		for k, val := range *endpoint.Settings.PgSettings {
			v.PgSettings[k] = fmt.Sprintf("%v", val)
		}
	}
	v.Disabled = types.BoolValue(endpoint.Disabled)
	if endpoint.Settings.PreloadLibraries != nil {
		v.EnabledLibraries = endpoint.Settings.PreloadLibraries.EnabledLibraries
		v.EnabledLibrariesUseDefaults = types.BoolPointerValue(endpoint.Settings.PreloadLibraries.UseDefaults)
	}
	v.PasswordlessAccess = types.BoolValue(endpoint.PasswordlessAccess)
	v.Host = types.StringValue(endpoint.Host)
	hostPooler := endpoint.ID + "-pooler" + strings.TrimPrefix(endpoint.Host, endpoint.ID)
	v.HostPooler = types.StringValue(hostPooler)
}

func NewNeonEndpointResource() resource.Resource {
	return &neonEndpointResource{}
}

func (r *neonEndpointResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_endpoint"
}

func (r *neonEndpointResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: `Project Endpoint. See details: https://neon.tech/docs/manage/endpoints/`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Endpoint ID.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description:   "Project ID.",
				Required:      true,
				PlanModifiers: requiresReplace,
			},
			"branch_id": schema.StringAttribute{
				Description:   "Branch ID.",
				Required:      true,
				PlanModifiers: requiresReplace,
			},
			"region_id": schema.StringAttribute{
				MarkdownDescription: `Cloud region where the resource's Postgres compute and storage reside.
Find the list here: https://neon.com/docs/introduction/regions.
Alternatively, use the data source ` + "`neon_active_regions`.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: requiresReplace,
			},
			"name": schema.StringAttribute{
				Description: "Name of the compute endpoint.",
				Optional:    true,
				Computed:    true,
			},
			"type": schema.StringAttribute{
				Description: "Compute endpoint type.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(neon.EndpointTypeReadWrite.String()),
				Validators:  []validator.String{validateEndpointType},
			},
			"autoscaling_limit_min_cu": schema.Float64Attribute{
				Description: "Minimum compute units for autoscaling.",
				Optional:    true,
				Computed:    true,
			},
			"autoscaling_limit_max_cu": schema.Float64Attribute{
				Description: "Maximum compute units for autoscaling.",
				Optional:    true,
				Computed:    true,
			},
			"suspend_timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: "Scale-to-zero idle timeout, in seconds, before the compute suspends.\n" +
					"   - `0` means the plan default (`300` sec.) applies;\n" +
					"   - `-1` means scale-to-zero is disabled (never suspends), can be set only on paid plans;\n" +
					"   - `604800` sec. (1 week) is the maximum.\n" +
					"   - `60` sec. (1 minute) is the minimum.\n" +
					"See details [here](https://neon.com/docs/manage/endpoints#scale-to-zero-configuration).",
				Validators: []validator.Int64{validateSuspendTimeout},
				Optional:   true,
				Computed:   true,
			},
			"compute_provisioner": schema.StringAttribute{
				Description: "Compute provisioner: `k8s-neonvm` (default) supports Autoscaling; `k8s-pod` is fixed-size compute. Also `docker` and `serverless-platform`.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("k8s-neonvm"),
				Validators:  []validator.String{validateComputeProvisioner},
			},
			"pg_settings": schema.MapAttribute{
				Description: "A raw representation of Postgres settings",
				Optional:    true,
				ElementType: types.StringType,
			},
			"disabled": schema.BoolAttribute{
				Description: `Whether to restrict connections to the compute endpoint.
	Enabling this option schedules a suspend compute operation.
	A disabled compute endpoint cannot be enabled by a connection or
	console action. However, the compute endpoint is periodically
	enabled by check_availability operations.`,
				Optional: true,
				Computed: true,
			},
			"enabled_libraries": schema.ListAttribute{
				Description: "Names of shared preload libraries to enable for the project.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"enabled_libraries_use_defaults": schema.BoolAttribute{
				Description: "When true, the project's preload libraries include the platform default set in addition to any libraries listed in `enabled_libraries`.",
				Optional:    true,
				Computed:    true,
			},
			"passwordless_access": schema.BoolAttribute{
				Description: "When true, the endpoint allows passwordless access.",
				Optional:    true,
				Computed:    true,
			},
			"host": schema.StringAttribute{
				Description: "The hostname of the compute endpoint.",
				Computed:    true,
			},
			"host_pooler": schema.StringAttribute{
				Description: "The hostname of the compute endpoint accessible through the connection pool (pgbouncer).",
				Computed:    true,
			},
		},
	}
}

var validateComputeProvisioner = validatorComputeProvisioner{}

type validatorComputeProvisioner struct{}

func (v validatorComputeProvisioner) Description(_ context.Context) string {
	return "Compute provisioner's validator."
}

func (v validatorComputeProvisioner) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validatorComputeProvisioner) ValidateString(ctx context.Context, req validator.StringRequest,
	resp *validator.StringResponse) {
	var data neonEndpointResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ComputeProvisioner.IsNull() {
		return
	}

	switch data.ComputeProvisioner.ValueString() {
	case "k8s-neonvm", "k8s-pod", "docker", "serverless-platform":
	default:
		resp.Diagnostics.AddError(`Invalid endpoint's "compute_provisioner"`,
			"Compute provisioner must be one of: k8s-neonvm, k8s-pod, docker, serverless-platform")
	}
}

var validateSuspendTimeout = validatorSuspendTimeout{}

type validatorSuspendTimeout struct{}

func (v validatorSuspendTimeout) Description(_ context.Context) string {
	return "Compute's scale-to-zero idle time validator."
}

func (v validatorSuspendTimeout) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validatorSuspendTimeout) ValidateInt64(ctx context.Context, req validator.Int64Request,
	resp *validator.Int64Response) {
	var data neonEndpointResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.SuspendTimeoutSeconds.IsNull() {
		return
	}

	val := data.SuspendTimeoutSeconds.ValueInt64()
	if val != 0 &&
		// never suspend
		val != -1 &&
		// custom
		(val < 60 || val > 604800) {
		resp.Diagnostics.AddError(`Invalid endpoint "suspend_timeout_seconds"`,
			"Suspend must be either 0 (default), -1 (never suspend), or between 60 and 604800 seconds.")
	}
}

var validateEndpointType = validatorEndpointType{}

type validatorEndpointType struct{}

func (v validatorEndpointType) Description(_ context.Context) string {
	return "Endpoint type validator."
}

func (v validatorEndpointType) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validatorEndpointType) ValidateString(ctx context.Context, req validator.StringRequest,
	resp *validator.StringResponse) {
	var data neonEndpointResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ComputeProvisioner.IsNull() {
		return
	}

	if _, err := neon.NewEndpointType(data.Type.ValueString()); err != nil {
		resp.Diagnostics.AddError(`Invalid endpoint "type"`, err.Error())
	}
}

func (r *neonEndpointResource) Configure(_ context.Context, req resource.ConfigureRequest,
	resp *resource.ConfigureResponse) {
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

func (r *neonEndpointResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonEndpointResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpointType, err := neon.NewEndpointType(state.Type.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode endpoint type", err.Error())
		return
	}

	cfg := neon.EndpointCreateRequestEndpoint{
		BranchID:    state.BranchID.ValueString(),
		Type:        endpointType,
		RegionID:    state.RegionID.ValueStringPointer(),
		Disabled:    state.Disabled.ValueBoolPointer(),
		Provisioner: pointer(neon.Provisioner(state.ComputeProvisioner.ValueString())),
		Name:        state.Name.ValueStringPointer(),
	}

	if !state.AutoscalingLimitMinCU.IsNull() && !state.AutoscalingLimitMinCU.IsUnknown() {
		cfg.AutoscalingLimitMinCu = pointer(neon.ComputeUnit(state.AutoscalingLimitMinCU.ValueFloat64()))
	}
	if !state.AutoscalingLimitMaxCU.IsNull() && !state.AutoscalingLimitMaxCU.IsUnknown() {
		cfg.AutoscalingLimitMaxCu = pointer(neon.ComputeUnit(state.AutoscalingLimitMaxCU.ValueFloat64()))
	}
	if !state.SuspendTimeoutSeconds.IsNull() && !state.SuspendTimeoutSeconds.IsUnknown() {
		cfg.SuspendTimeoutSeconds = pointer(neon.SuspendTimeoutSeconds(state.SuspendTimeoutSeconds.ValueInt64()))
	}

	if len(state.PgSettings) > 0 {
		var pgSettings = make(neon.PgSettingsData, len(state.PgSettings))
		for k, v := range state.PgSettings {
			pgSettings[k] = v
		}
		cfg.Settings = &neon.EndpointSettingsData{PgSettings: &pgSettings}
	}

	if len(state.EnabledLibraries) > 0 {
		if cfg.Settings == nil {
			cfg.Settings = new(neon.EndpointSettingsData)
		}
		cfg.Settings.PreloadLibraries = &neon.PreloadLibraries{
			EnabledLibraries: state.EnabledLibraries,
		}
	}
	if !state.EnabledLibrariesUseDefaults.IsUnknown() && !state.EnabledLibrariesUseDefaults.IsNull() {
		if cfg.Settings == nil {
			cfg.Settings = new(neon.EndpointSettingsData)
		}
		if cfg.Settings.PreloadLibraries == nil {
			cfg.Settings.PreloadLibraries = new(neon.PreloadLibraries)
		}
		cfg.Settings.PreloadLibraries.UseDefaults = state.EnabledLibrariesUseDefaults.ValueBoolPointer()
	}

	var endpoint neon.Endpoint
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
		result, err := r.client.CreateProjectEndpoint(
			state.ProjectID.ValueString(),
			neon.EndpointCreateRequest{Endpoint: cfg},
		)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
		endpoint = result.EndpointResponse.Endpoint
		return nil
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.inferAttr(endpoint)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonEndpointResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonEndpointResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var endpoint neon.Endpoint
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(_ context.Context) error {
		result, err := r.client.GetProjectEndpoint(state.ProjectID.ValueString(), state.ID.ValueString())
		if err == nil {
			endpoint = result.Endpoint
		}
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	if endpoint.ID == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(endpoint)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonEndpointResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan, prior neonEndpointResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var cfg neon.EndpointUpdateRequestEndpoint

	if !prior.BranchID.Equal(plan.BranchID) {
		cfg.BranchID = pointer(plan.BranchID.ValueString())
	}

	if !prior.Name.Equal(plan.Name) && plan.Name.ValueString() != "" {
		cfg.Name = plan.Name.ValueStringPointer()
	}

	if !plan.Disabled.IsUnknown() && !prior.Disabled.Equal(plan.Disabled) {
		cfg.Disabled = plan.Disabled.ValueBoolPointer()
	}

	if !plan.PasswordlessAccess.IsUnknown() && !prior.PasswordlessAccess.Equal(plan.PasswordlessAccess) {
		cfg.PasswordlessAccess = plan.PasswordlessAccess.ValueBoolPointer()
	}

	// Update autoscaling limits if its new value is known and differs from the prev.
	if !plan.AutoscalingLimitMinCU.IsNull() && !plan.AutoscalingLimitMinCU.IsUnknown() &&
		!prior.AutoscalingLimitMinCU.Equal(plan.AutoscalingLimitMinCU) {
		cfg.AutoscalingLimitMinCu = pointer(neon.ComputeUnit(plan.AutoscalingLimitMinCU.ValueFloat64()))
	}

	if !plan.AutoscalingLimitMaxCU.IsNull() && !plan.AutoscalingLimitMaxCU.IsUnknown() &&
		!prior.AutoscalingLimitMaxCU.Equal(plan.AutoscalingLimitMaxCU) {
		cfg.AutoscalingLimitMaxCu = pointer(neon.ComputeUnit(plan.AutoscalingLimitMaxCU.ValueFloat64()))
	}

	// Update auto suspension timeout if its new value differs from the prev.
	// Note that the new value may be unknown, that will set the timeout to the default value because
	// the zero value corresponds to the default Neon settings
	if !plan.SuspendTimeoutSeconds.IsNull() && !plan.SuspendTimeoutSeconds.IsUnknown() &&
		!prior.SuspendTimeoutSeconds.Equal(plan.SuspendTimeoutSeconds) {
		cfg.SuspendTimeoutSeconds = pointer(neon.SuspendTimeoutSeconds(plan.SuspendTimeoutSeconds.ValueInt64()))
	}

	if !prior.ComputeProvisioner.Equal(plan.ComputeProvisioner) {
		cfg.Provisioner = pointer(neon.Provisioner(plan.ComputeProvisioner.ValueString()))
	}

	if !reflect.DeepEqual(prior.PgSettings, plan.PgSettings) {
		pgSettings := make(neon.PgSettingsData, len(plan.PgSettings))
		for k, v := range plan.PgSettings {
			pgSettings[k] = v
		}
		cfg.Settings = &neon.EndpointSettingsData{PgSettings: &pgSettings}
	}

	slices.SortStableFunc(prior.EnabledLibraries, func(a, b string) int {
		return strings.Compare(a, b)
	})
	slices.SortStableFunc(plan.EnabledLibraries, func(a, b string) int {
		return strings.Compare(a, b)
	})
	if plan.EnabledLibraries != nil && !slices.Equal(prior.EnabledLibraries, plan.EnabledLibraries) {
		if cfg.Settings == nil {
			cfg.Settings = new(neon.EndpointSettingsData)
		}
		libs := plan.EnabledLibraries
		// initialize the preload libraries as empty slice, so the request payload
		// contains the value `[]` after serialization
		if len(libs) == 0 {
			libs = []string{}
		}
		cfg.Settings.PreloadLibraries = &neon.PreloadLibraries{
			EnabledLibraries: libs,
		}
	}

	if !plan.EnabledLibrariesUseDefaults.IsNull() &&
		!prior.EnabledLibrariesUseDefaults.Equal(plan.EnabledLibrariesUseDefaults) {
		if cfg.Settings == nil {
			cfg.Settings = new(neon.EndpointSettingsData)
		}
		if cfg.Settings.PreloadLibraries == nil {
			cfg.Settings.PreloadLibraries = new(neon.PreloadLibraries)
		}
		cfg.Settings.PreloadLibraries.UseDefaults = plan.EnabledLibrariesUseDefaults.ValueBoolPointer()
	}

	var endpoint neon.Endpoint
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
		result, err := r.client.UpdateProjectEndpoint(
			prior.ProjectID.ValueString(),
			prior.ID.ValueString(),
			neon.EndpointUpdateRequest{Endpoint: cfg},
		)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
		endpoint = result.EndpointResponse.Endpoint
		return nil
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.inferAttr(endpoint)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonEndpointResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonEndpointResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		result, err := r.client.DeleteProjectEndpoint(state.ProjectID.ValueString(), state.ID.ValueString())
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
		return nil
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *neonEndpointResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 2)
	if len(els) != 2 || els[0] == "" || els[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid Neon Endpoint Import ID",
			"Expected an import ID in the form <project_id>/<endpoint_id>.",
		)
		return
	}

	state := neonEndpointResourceModel{
		ID:        types.StringValue(els[1]),
		ProjectID: types.StringValue(els[0]),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
}
