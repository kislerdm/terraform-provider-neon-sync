package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-mux/tf5to6server"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	sdkresource "github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	sdkschema "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/require"
)

func branchRolesDataSource(t *testing.T) datasource.DataSourceWithConfigure {
	t.Helper()
	for _, factory := range (&frameworkProvider{}).DataSources(context.Background()) {
		d := factory()
		var metadata datasource.MetadataResponse
		d.Metadata(context.Background(), datasource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_branch_roles" {
			configured, ok := d.(datasource.DataSourceWithConfigure)
			require.True(t, ok)
			return configured
		}
	}
	require.FailNow(t, "neon_branch_roles must have a Framework registration owner")
	return nil
}

func branchRolesSchema(t *testing.T, d datasource.DataSource) schema.Schema {
	t.Helper()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return resp.Schema
}

func branchRolesConfig(s schema.Schema, projectID, branchID tftypes.Value) tfsdk.Config {
	return tfsdk.Config{
		Schema: s,
		Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), map[string]tftypes.Value{
			"id":         tftypes.NewValue(tftypes.String, nil),
			"project_id": projectID,
			"branch_id":  branchID,
			"roles":      tftypes.NewValue(s.Attributes["roles"].GetType().TerraformType(context.Background()), nil),
		}),
	}
}

func TestBranchRolesRead(t *testing.T) {
	d := branchRolesDataSource(t)
	s := branchRolesSchema(t, d)
	client := newBranchDataSourceTestClient(t,
		"/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/roles", http.StatusOK,
		`{"roles":[{"branch_id":"br-cool-moon-42","name":"reader","protected":false,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`)
	var configured datasource.ConfigureResponse
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError(), "%v", configured.Diagnostics)

	resp := datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{Config: branchRolesConfig(s,
		tftypes.NewValue(tftypes.String, "cool-moon-42"),
		tftypes.NewValue(tftypes.String, "br-cool-moon-42"))}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var id string
	require.Empty(t, resp.State.GetAttribute(context.Background(), path.Root("id"), &id))
	require.Equal(t, "cool-moon-42/br-cool-moon-42/roles", id)
}

type branchRolesTestModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Roles     types.List   `tfsdk:"roles"`
}

type branchRolesTestRole struct {
	Name      types.String `tfsdk:"name"`
	Protected types.Bool   `tfsdk:"protected"`
}

func configuredBranchRoles(t *testing.T, client *neon.Client) (datasource.DataSourceWithConfigure, schema.Schema) {
	t.Helper()
	d := branchRolesDataSource(t)
	var configured datasource.ConfigureResponse
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError(), "%v", configured.Diagnostics)
	return d, branchRolesSchema(t, d)
}

func TestBranchRolesMapping(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want []branchRolesTestRole
	}{
		{
			name: "ordered roles with explicit and missing protection",
			body: `{"roles":[
				{"branch_id":"response-scope","name":"writer.with-dash","protected":false,"password":"do-not-store","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
				{"branch_id":"response-scope","name":"system","protected":true,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
				{"branch_id":"response-scope","name":"omitted","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
				{"branch_id":"response-scope","name":"null","protected":null,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}
			]}`,
			want: []branchRolesTestRole{
				{Name: types.StringValue("writer.with-dash"), Protected: types.BoolValue(false)},
				{Name: types.StringValue("system"), Protected: types.BoolValue(true)},
				{Name: types.StringValue("omitted"), Protected: types.BoolValue(true)},
				{Name: types.StringValue("null"), Protected: types.BoolValue(true)},
			},
		},
		{name: "empty list", body: `{"roles":[]}`, want: []branchRolesTestRole{}},
		{name: "nil SDK slice", body: `{"roles":null}`, want: []branchRolesTestRole{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newBranchDataSourceTestClient(t,
				"/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/roles", http.StatusOK, tt.body)
			d, s := configuredBranchRoles(t, client)
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: s}}
			d.Read(context.Background(), datasource.ReadRequest{Config: branchRolesConfig(s,
				tftypes.NewValue(tftypes.String, "cool-moon-42"), tftypes.NewValue(tftypes.String, "br-cool-moon-42"))}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			var got branchRolesTestModel
			require.Empty(t, resp.State.Get(context.Background(), &got))
			require.Equal(t, "cool-moon-42/br-cool-moon-42/roles", got.ID.ValueString())
			require.Equal(t, "cool-moon-42", got.ProjectID.ValueString())
			require.Equal(t, "br-cool-moon-42", got.BranchID.ValueString())
			require.False(t, got.Roles.IsNull())
			roles := []branchRolesTestRole{}
			require.Empty(t, got.Roles.ElementsAs(context.Background(), &roles, false))
			require.Equal(t, tt.want, roles)
			require.NotContains(t, resp.State.Raw.String(), "do-not-store")
		})
	}
}

type branchRolesHTTPFunc func(*http.Request) (*http.Response, error)

func (f branchRolesHTTPFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBranchRolesReadErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		err    error
		want   string
	}{
		{name: "server rejects known input", status: 400, body: `{"code":"INVALID_INPUT","message":"upstream rejected input","request_id":"discarded"}`, want: "[HTTP Code: 400][Error Code: INVALID_INPUT] upstream rejected input"},
		{name: "permission denied", status: 403, body: `{"code":"FORBIDDEN","message":"permission denied"}`, want: "[HTTP Code: 403][Error Code: FORBIDDEN] permission denied"},
		{name: "any not found reason", status: 404, body: `{"code":"NOT_FOUND","message":"not found","reason":"arbitrary"}`, want: "[HTTP Code: 404][Error Code: NOT_FOUND] not found"},
		{name: "transport", err: errors.New("transport failed"), want: "transport failed"},
		{name: "invalid success JSON", status: 200, body: `{`, want: "unexpected end of JSON input"},
		{name: "invalid error JSON", status: 404, body: `{`, want: "[HTTP Code: 404][Error Code: ] unexpected end of JSON input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := neon.NewClient(neon.Config{Key: "test", HTTPClient: branchRolesHTTPFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "/api/v2/projects/known-input/branches/not-a-branch/roles", req.URL.Path)
				if tt.err != nil {
					return nil, tt.err
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})})
			require.NoError(t, err)
			d, s := configuredBranchRoles(t, client)
			prior, err := (tfprotov6.DynamicValue{JSON: []byte(`{"id":"known-input/not-a-branch/roles","project_id":"known-input","branch_id":"not-a-branch","roles":[{"name":"prior","protected":false}]}`)}).Unmarshal(s.Type().TerraformType(context.Background()))
			require.NoError(t, err)
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
			d.Read(context.Background(), datasource.ReadRequest{Config: branchRolesConfig(s,
				tftypes.NewValue(tftypes.String, "known-input"), tftypes.NewValue(tftypes.String, "not-a-branch"))}, &resp)
			require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
			require.Equal(t, "Neon API request failed", resp.Diagnostics[0].Summary())
			require.Equal(t, tt.want, resp.Diagnostics[0].Detail())
			require.True(t, prior.Equal(resp.State.Raw), "failed reads must preserve prior state")
		})
	}
}

func TestBranchRolesConfigure(t *testing.T) {
	for _, tt := range []struct {
		name string
		data interface{}
		want bool
	}{
		{name: "validation without provider data"},
		{name: "wrong provider data", data: "wrong", want: true},
		{name: "typed nil adapter", data: (*providerAdapter)(nil), want: true},
		{name: "nil SDK client", data: &providerAdapter{}, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := branchRolesDataSource(t)
			var resp datasource.ConfigureResponse
			d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: tt.data}, &resp)
			require.Equal(t, tt.want, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		})
	}
	d := branchRolesDataSource(t)
	s := branchRolesSchema(t, d)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), datasource.ReadRequest{Config: branchRolesConfig(s,
		tftypes.NewValue(tftypes.String, "cool-moon-42"), tftypes.NewValue(tftypes.String, "br-cool-moon-42"))}, &resp)
	require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
	require.Equal(t, "SDK is not configured", resp.Diagnostics[0].Summary())
	require.True(t, resp.State.Raw.IsNull())
}

func TestBranchRolesUnknownScope(t *testing.T) {
	client, err := neon.NewClient(neon.Config{Key: "test", HTTPClient: branchRolesHTTPFunc(func(*http.Request) (*http.Response, error) {
		t.Error("null/unknown scope must not reach the SDK")
		return nil, errors.New("unexpected request")
	})})
	require.NoError(t, err)
	for _, name := range []string{"project_id", "branch_id"} {
		for _, value := range []struct {
			name string
			raw  interface{}
		}{
			{name: "null", raw: nil},
			{name: "unknown", raw: tftypes.UnknownValue},
		} {
			t.Run(name+"/"+value.name, func(t *testing.T) {
				d, s := configuredBranchRoles(t, client)
				project := tftypes.NewValue(tftypes.String, "cool-moon-42")
				branch := tftypes.NewValue(tftypes.String, "br-cool-moon-42")
				if name == "project_id" {
					project = tftypes.NewValue(tftypes.String, value.raw)
				} else {
					branch = tftypes.NewValue(tftypes.String, value.raw)
				}
				resp := datasource.ReadResponse{State: tfsdk.State{Schema: s}}
				d.Read(context.Background(), datasource.ReadRequest{Config: branchRolesConfig(s, project, branch)}, &resp)
				require.True(t, resp.Diagnostics.HasError())
				require.True(t, resp.State.Raw.IsNull())
			})
		}
	}
}

func TestBranchRolesRetry(t *testing.T) {
	for _, status := range []int{423, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client, err := neon.NewClient(neon.Config{Key: "test", HTTPClient: branchRolesHTTPFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/roles", req.URL.Path)
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"code":"RETRY","message":"try again"}`))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"roles":[]}`))}, nil
			})})
			require.NoError(t, err)
			d, s := configuredBranchRoles(t, client)
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: s}}
			d.Read(context.Background(), datasource.ReadRequest{Config: branchRolesConfig(s,
				tftypes.NewValue(tftypes.String, "cool-moon-42"), tftypes.NewValue(tftypes.String, "br-cool-moon-42"))}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, 2, calls)
			var got branchRolesTestModel
			require.Empty(t, resp.State.Get(context.Background(), &got))
			require.Equal(t, "cool-moon-42/br-cool-moon-42/roles", got.ID.ValueString())
		})
	}
}

// The frozen schema comes from 9a5cf810; retaining it in tests avoids duplicate runtime registration.
func legacyBranchRolesSchema() *sdkschema.Resource {
	return &sdkschema.Resource{
		SchemaVersion: 1,
		ReadContext:   func(context.Context, *sdkschema.ResourceData, interface{}) diag.Diagnostics { return nil },
		Schema: map[string]*sdkschema.Schema{
			"project_id": {Type: sdkschema.TypeString, Required: true},
			"branch_id":  {Type: sdkschema.TypeString, Required: true},
			"roles": {
				Type: sdkschema.TypeList, Optional: true,
				Elem: &sdkschema.Resource{Schema: map[string]*sdkschema.Schema{
					"name":      {Type: sdkschema.TypeString, Computed: true},
					"protected": {Type: sdkschema.TypeBool, Computed: true},
				}},
			},
		},
	}
}

func TestBranchRolesLegacySchemaAndState(t *testing.T) {
	ctx := context.Background()
	legacy := &sdkschema.Provider{DataSourcesMap: map[string]*sdkschema.Resource{"neon_branch_roles": legacyBranchRolesSchema()}}
	oldServer, err := tf5to6server.UpgradeServer(ctx, func() tfprotov5.ProviderServer { return legacy.GRPCProvider() })
	require.NoError(t, err)
	oldResp, err := oldServer.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	require.Empty(t, oldResp.Diagnostics)
	old := oldResp.DataSourceSchemas["neon_branch_roles"]

	client := newBranchDataSourceTestClient(t,
		"/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/roles", 200,
		`{"roles":[{"branch_id":"br-cool-moon-42","name":"reader","protected":false,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`)
	server := providerserver.NewProtocol6(&frameworkProvider{version: "unitTest", neonAdapter: &providerAdapter{sdk: client}})()
	newResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	require.Empty(t, newResp.Diagnostics)
	current := newResp.DataSourceSchemas["neon_branch_roles"]
	require.NotNil(t, current)
	require.True(t, old.ValueType().Equal(current.ValueType()), "list/object state types must remain compatible")
	require.Equal(t, int64(1), old.Version)
	require.Equal(t, int64(0), current.Version)
	for _, oldAttr := range old.Block.Attributes {
		var found *tfprotov6.SchemaAttribute
		for _, attr := range current.Block.Attributes {
			if attr.Name == oldAttr.Name {
				found = attr
			}
		}
		require.NotNil(t, found)
		require.Equal(t, oldAttr.Required, found.Required, found.Name)
		require.Equal(t, oldAttr.Optional, found.Optional, found.Name)
		require.Equal(t, oldAttr.Computed, found.Computed, found.Name)
		require.Equal(t, oldAttr.Sensitive, found.Sensitive, found.Name)
		require.True(t, oldAttr.Type.Equal(found.Type), found.Name)
	}
	d := branchRolesDataSource(t)
	s := branchRolesSchema(t, d)
	roles := s.Attributes["roles"].(schema.ListNestedAttribute)
	require.True(t, roles.Computed)
	require.False(t, roles.Optional)
	for name, attr := range roles.NestedObject.Attributes {
		require.True(t, attr.IsComputed(), name)
		require.False(t, attr.IsSensitive(), name)
	}
	for _, attr := range legacyBranchRolesSchema().Schema {
		require.Nil(t, attr.Default)
		require.Nil(t, attr.ValidateFunc)
		require.False(t, attr.ForceNew)
	}

	state := []byte(`{"id":"cool-moon-42/br-cool-moon-42/roles","project_id":"cool-moon-42","branch_id":"br-cool-moon-42","roles":[{"name":"reader","protected":false}]}`)
	oldState, err := (tfprotov6.DynamicValue{JSON: state}).Unmarshal(old.ValueType())
	require.NoError(t, err)
	decoded, err := (tfprotov6.DynamicValue{JSON: state}).Unmarshal(current.ValueType())
	require.NoError(t, err)
	require.True(t, oldState.Equal(decoded))
	providerConfig, err := tfprotov6.NewDynamicValue(newResp.Provider.ValueType(), tftypes.NewValue(newResp.Provider.ValueType(), map[string]tftypes.Value{"api_key": tftypes.NewValue(tftypes.String, nil)}))
	require.NoError(t, err)
	configured, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &providerConfig})
	require.NoError(t, err)
	require.Empty(t, configured.Diagnostics)
	config := branchRolesConfig(s, tftypes.NewValue(tftypes.String, "cool-moon-42"), tftypes.NewValue(tftypes.String, "br-cool-moon-42"))
	raw, err := tfprotov6.NewDynamicValue(current.ValueType(), config.Raw)
	require.NoError(t, err)
	read, err := server.ReadDataSource(ctx, &tfprotov6.ReadDataSourceRequest{TypeName: "neon_branch_roles", Config: &raw})
	require.NoError(t, err)
	require.Empty(t, read.Diagnostics)
	refreshed, err := read.State.Unmarshal(current.ValueType())
	require.NoError(t, err)
	require.True(t, decoded.Equal(refreshed), "refreshed fixture values must match legacy state")
}

func TestBranchRolesMux(t *testing.T) {
	ctx := context.Background()
	server, err := NewServer("unitTest")
	require.NoError(t, err)
	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Diagnostics)
	for _, name := range []string{"neon_branch_roles", "neon_branches", "neon_branch_endpoints", "neon_project", "neon_active_regions"} {
		require.Contains(t, resp.DataSourceSchemas, name)
	}
	require.NotContains(t, p.DataSourcesMap, "neon_branch_roles")
	config, err := tfprotov6.NewDynamicValue(resp.Provider.ValueType(), tftypes.NewValue(resp.Provider.ValueType(), map[string]tftypes.Value{"api_key": tftypes.NewValue(tftypes.String, "fixture-key")}))
	require.NoError(t, err)
	configured, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &config})
	require.NoError(t, err)
	require.Empty(t, configured.Diagnostics)
}

func branchRolesHCL() string {
	return `data "neon_branch_roles" "this" {
  project_id = "cool-moon-42"
  branch_id  = "br-cool-moon-42"
}`
}

func TestBranchRolesTerraform(t *testing.T) {
	client := newBranchDataSourceTestClient(t,
		"/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/roles", 200,
		`{"roles":[{"branch_id":"br-cool-moon-42","name":"reader","protected":false,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`)
	for _, tt := range []struct {
		name   string
		config string
		want   *regexp.Regexp
	}{
		{name: "existing input-only HCL", config: branchRolesHCL()},
		{name: "legacy optional ID", config: strings.TrimSuffix(branchRolesHCL(), "}") + "id = \"legacy-configured-id\"\n}"},
		{name: "reject legacy output block", config: strings.TrimSuffix(branchRolesHCL(), "}") + "roles {}\n}", want: regexp.MustCompile("Unsupported block type")},
		{name: "reject configured list output", config: strings.TrimSuffix(branchRolesHCL(), "}") + "roles = []\n}", want: regexp.MustCompile("(unconfigurable attribute|read-only attribute)")},
		{name: "missing required project", config: `data "neon_branch_roles" "this" { branch_id = "br-cool-moon-42" }`, want: regexp.MustCompile("Missing required argument")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			step := sdkresource.TestStep{Config: tt.config, ExpectError: tt.want}
			if tt.want == nil {
				step.Check = sdkresource.ComposeAggregateTestCheckFunc(
					sdkresource.TestCheckResourceAttr("data.neon_branch_roles.this", "id", "cool-moon-42/br-cool-moon-42/roles"),
					sdkresource.TestCheckResourceAttr("data.neon_branch_roles.this", "roles.0.name", "reader"),
					sdkresource.TestCheckResourceAttr("data.neon_branch_roles.this", "roles.0.protected", "false"),
				)
			}
			sdkresource.UnitTest(t, sdkresource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps:                    []sdkresource.TestStep{step},
			})
		})
	}
}
