package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestBranchStorageDataSourceRegisteredWithFrameworkProvider(t *testing.T) {
	p := &frameworkProvider{}
	dataSources := p.DataSources(context.Background())

	var found datasource.DataSource
	for _, factory := range dataSources {
		candidate := factory()
		var metadata datasource.MetadataResponse
		candidate.Metadata(context.Background(), datasource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_branch_storage" {
			found = candidate
			break
		}
	}

	require.NotNil(t, found, "neon_branch_storage must be registered as a Framework data source")
}

func TestBranchStorageDataSourceSchema(t *testing.T) {
	d := NewBranchStorageDataSource()
	var response datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &response)

	require.Zero(t, response.Diagnostics.ErrorsCount())
	require.Contains(t, response.Schema.Attributes, "project_id")
	require.Contains(t, response.Schema.Attributes, "branch_id")
	require.Contains(t, response.Schema.Attributes, "s3_endpoint")
	require.Contains(t, response.Schema.Attributes, "region")
	require.Contains(t, response.Schema.Attributes, "force_path_style")
	require.NotContains(t, response.Schema.Attributes, "enabled")
}

func TestBranchStorageDataSourceModelUsesFrameworkTypes(t *testing.T) {
	model := neonBranchStorageDataSourceModel{
		ID:             types.StringValue("project/branch"),
		ProjectID:      types.StringValue("project"),
		BranchID:       types.StringValue("branch"),
		S3Endpoint:     types.StringValue("https://example.invalid"),
		Region:         types.StringValue("us-east-1"),
		ForcePathStyle: types.BoolValue(true),
	}

	require.Equal(t, "project/branch", model.ID.ValueString())
	require.True(t, model.ForcePathStyle.ValueBool())
}

type stubHTTPClient struct {
	status int
	body   string
}

func (s stubHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func newBranchStorageDataSourceWithStub(t *testing.T, status int, body string) *neonBranchStorageDataSource {
	t.Helper()
	client, err := neon.NewClient(neon.Config{
		Key:        "test",
		HTTPClient: stubHTTPClient{status: status, body: body},
	})
	require.NoError(t, err)
	return &neonBranchStorageDataSource{client: client}
}

func branchStorageTestSchema(t *testing.T) schema.Schema {
	t.Helper()
	d := NewBranchStorageDataSource()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	require.Zero(t, resp.Diagnostics.ErrorsCount())
	return resp.Schema
}

func branchStorageReadConfig(t *testing.T, projectID, branchID string) tfsdk.Config {
	t.Helper()
	s := branchStorageTestSchema(t)
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	return tfsdk.Config{
		Schema: s,
		Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":               tftypes.NewValue(tftypes.String, nil),
			"project_id":       tftypes.NewValue(tftypes.String, projectID),
			"branch_id":        tftypes.NewValue(tftypes.String, branchID),
			"s3_endpoint":      tftypes.NewValue(tftypes.String, nil),
			"region":           tftypes.NewValue(tftypes.String, nil),
			"force_path_style": tftypes.NewValue(tftypes.Bool, nil),
		}),
	}
}

func TestBranchStorageDataSourceReadSuccess(t *testing.T) {
	d := newBranchStorageDataSourceWithStub(t, http.StatusOK, `{
		"enabled": true,
		"force_path_style": true,
		"region": "us-east-2",
		"s3_endpoint": "https://br-cool-moon-42.storage.c-2.local.neon.build"
	}`)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: branchStorageTestSchema(t)}}
	d.Configure(context.Background(),
		datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: d.client}},
		&datasource.ConfigureResponse{})

	d.Read(context.Background(),
		datasource.ReadRequest{Config: branchStorageReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.Equal(t, 0, resp.Diagnostics.ErrorsCount(), "diagnostics: %v", resp.Diagnostics)
	var got neonBranchStorageDataSourceModel
	stateDiags := resp.State.Get(context.Background(), &got)
	require.Equal(t, 0, len(stateDiags), "state diagnostics: %v", stateDiags)

	require.Equal(t, "cool-moon-42/br-cool-moon-42", got.ID.ValueString())
	require.Equal(t, "https://br-cool-moon-42.storage.c-2.local.neon.build", got.S3Endpoint.ValueString())
	require.Equal(t, "us-east-2", got.Region.ValueString())
	require.True(t, got.ForcePathStyle.ValueBool())
}

func TestBranchStorageDataSourceReadStorageNotEnabled(t *testing.T) {
	for _, reason := range []string{
		"org_not_entitled",
		"region_unavailable",
		"branch_directory_missing",
		"branch_not_found",
	} {
		t.Run(reason, func(t *testing.T) {
			d := newBranchStorageDataSourceWithStub(t, http.StatusNotFound, `{
				"code": "STORAGE_NOT_ENABLED",
				"message": "storage unavailable",
				"reason": "`+reason+`"
			}`)

			resp := &datasource.ReadResponse{State: tfsdk.State{Schema: branchStorageTestSchema(t)}}
			d.Configure(context.Background(),
				datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: d.client}},
				&datasource.ConfigureResponse{})

			d.Read(context.Background(),
				datasource.ReadRequest{Config: branchStorageReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
				resp)

			require.NotZero(t, resp.Diagnostics.ErrorsCount(), "expected an actionable diagnostic for storage unavailable")
			combined := ""
			for _, diag := range resp.Diagnostics {
				combined += diag.Summary() + ": " + diag.Detail() + "\n"
			}
			require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
			require.Equal(t, "Neon API request failed", resp.Diagnostics[0].Summary())
			require.Equal(t, "[HTTP Code: 404][Error Code: STORAGE_NOT_ENABLED] storage unavailable", resp.Diagnostics[0].Detail())
			require.NotContains(t, combined, reason)
		})
	}
}

func TestBranchStorageDataSourceReadAPIError(t *testing.T) {
	d := newBranchStorageDataSourceWithStub(t, http.StatusBadRequest, `{
		"code": "INVALID_INPUT",
		"message": "upstream rejected request"
	}`)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: branchStorageTestSchema(t)}}
	d.Configure(context.Background(),
		datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: d.client}},
		&datasource.ConfigureResponse{})

	d.Read(context.Background(),
		datasource.ReadRequest{Config: branchStorageReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.NotZero(t, resp.Diagnostics.ErrorsCount(), "expected an actionable diagnostic for non-2xx API error")
	combined := ""
	for _, diag := range resp.Diagnostics {
		combined += diag.Summary() + ": " + diag.Detail() + "\n"
	}
	require.Contains(t, combined, "INVALID_INPUT")
	require.Equal(t, "[HTTP Code: 400][Error Code: INVALID_INPUT] upstream rejected request", resp.Diagnostics[0].Detail())
}

func TestBranchStorageDataSourceReadWithoutConfigure(t *testing.T) {
	d := &neonBranchStorageDataSource{}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: branchStorageTestSchema(t)}}

	d.Read(context.Background(),
		datasource.ReadRequest{Config: branchStorageReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.NotZero(t, resp.Diagnostics.ErrorsCount(), "expected a diagnostic when SDK is not configured")
}

type branchStorageHTTPFunc func(*http.Request) (*http.Response, error)

func (f branchStorageHTTPFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBranchStorageDataSourceReadWithConfiguredProvider(t *testing.T) {
	// Exercise the real provider wiring without sending requests to Neon.
	// Keep this test serial because http.DefaultTransport is process-wide.
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	http.DefaultTransport = branchStorageHTTPFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/api/v2/projects/project/branches/branch/storage", req.URL.Path)
		require.Equal(t, "Bearer test", req.Header.Get("Authorization"))
		require.Contains(t, req.Header.Get("User-Agent"), "tfProvider-neondatabase/neon@test")
		return stubHTTPClient{
			status: http.StatusNotFound,
			body:   `{"code":"STORAGE_NOT_ENABLED","message":"storage unavailable","reason":"org_not_entitled"}`,
		}.Do(req)
	})

	ctx := context.Background()
	p := &frameworkProvider{version: "test"}
	var schemaResp frameworkprovider.SchemaResponse
	p.Schema(ctx, frameworkprovider.SchemaRequest{}, &schemaResp)
	var configureResp frameworkprovider.ConfigureResponse
	p.Configure(ctx, frameworkprovider.ConfigureRequest{
		TerraformVersion: "test",
		Config: tfsdk.Config{
			Schema: schemaResp.Schema,
			Raw: tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), map[string]tftypes.Value{
				"api_key": tftypes.NewValue(tftypes.String, "test"),
			}),
		},
	}, &configureResp)
	require.False(t, configureResp.Diagnostics.HasError(), "%v", configureResp.Diagnostics)

	d := NewBranchStorageDataSource().(*neonBranchStorageDataSource)
	var dataSourceConfigureResp datasource.ConfigureResponse
	d.Configure(ctx, datasource.ConfigureRequest{ProviderData: configureResp.DataSourceData}, &dataSourceConfigureResp)
	require.False(t, dataSourceConfigureResp.Diagnostics.HasError(), "%v", dataSourceConfigureResp.Diagnostics)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: branchStorageTestSchema(t)}}
	d.Read(ctx, datasource.ReadRequest{Config: branchStorageReadConfig(t, "project", "branch")}, resp)
	require.True(t, resp.Diagnostics.HasError())
	require.Equal(t, "[HTTP Code: 404][Error Code: STORAGE_NOT_ENABLED] storage unavailable", resp.Diagnostics[0].Detail())
}
