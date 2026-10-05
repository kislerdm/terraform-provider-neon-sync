package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	sdkresource "github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestDataAPIConfigureAndMissingClient(t *testing.T) {
	for _, input := range []any{nil, "wrong type", (*providerAdapter)(nil), &providerAdapter{}} {
		t.Run(fmt.Sprintf("%T", input), func(t *testing.T) {
			r := dataAPITestResource(t)
			var configured resource.ConfigureResponse
			r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: input}, &configured)
			require.Equal(t, input != nil, configured.Diagnostics.HasError())
			state := dataAPIState(t, nil)
			created := resource.CreateResponse{State: state}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &created)
			require.True(t, created.Diagnostics.HasError())
			read := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &read)
			require.True(t, read.Diagnostics.HasError())
			updated := resource.UpdateResponse{State: state}
			r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(state), State: state}, &updated)
			require.True(t, updated.Diagnostics.HasError())
			deleted := resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, &deleted)
			require.True(t, deleted.Diagnostics.HasError())
		})
	}
}

func TestDataAPIFailedUpdateRetainsRecoverableState(t *testing.T) {
	for _, followUp := range []bool{false, true} {
		t.Run(fmt.Sprint(followUp), func(t *testing.T) {
			calls := []dataAPITestCall{{method: "PATCH", status: 403, body: `{"code":"DENIED","message":"unchanged server error"}`}}
			if followUp {
				calls = []dataAPITestCall{{method: "PATCH", status: 201, body: `{}`}, {method: "GET", status: 403, body: `{"code":"DENIED","message":"unchanged server error"}`}}
			}
			r := dataAPIConfiguredResource(t, calls...)
			prior := dataAPIState(t, map[string]any{"settings": map[string]any{"db_max_rows": float64(10)}})
			plan := dataAPIState(t, map[string]any{"id": tftypes.UnknownValue, "url": tftypes.UnknownValue, "settings": map[string]any{"db_max_rows": float64(20)}})
			resp := resource.UpdateResponse{State: prior}
			r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(plan), State: prior}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			require.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "unchanged server error")
			require.Equal(t, "pr-test/br-test/neondb", dataAPIString(t, resp.State, "id"))
			require.Equal(t, "https://endpoint.test/rest/v1", dataAPIString(t, resp.State, "url"))
			var rows types.Int64
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings").AtName("db_max_rows"), &rows).HasError())
			wantRows := int64(10)
			if followUp {
				wantRows = 20
			}
			require.Equal(t, wantRows, rows.ValueInt64())
		})
	}
}

func TestDataAPIRetryAndReadErrors(t *testing.T) {
	t.Run("retry transient response", func(t *testing.T) {
		r := dataAPIConfiguredResource(t, dataAPITestCall{method: "GET", status: 429, body: `{"code":"LIMIT","message":"retry"}`}, dataAPITestCall{method: "GET", status: 200, body: dataAPIGetBody})
		state := dataAPIState(t, nil)
		resp := resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	})
	for _, method := range []string{"GET", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			r := dataAPIConfiguredResource(t, dataAPITestCall{method: method, status: 403, body: `{"code":"DENIED","message":"access refused"}`})
			state := dataAPIState(t, nil)
			if method == "GET" {
				resp := resource.ReadResponse{State: state}
				r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
				require.True(t, resp.Diagnostics.HasError())
				require.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "access refused")
				require.False(t, resp.State.Raw.IsNull())
			} else {
				resp := resource.DeleteResponse{State: state}
				r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
				require.True(t, resp.Diagnostics.HasError())
				require.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "access refused")
				require.False(t, resp.State.Raw.IsNull())
			}
		})
	}
}

func TestDataAPIMutationRetries(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			body := `{}`
			if method == "POST" {
				body = `{"url":"https://endpoint.test/rest/v1"}`
			}
			calls := []dataAPITestCall{
				{method: method, status: 429, body: `{"code":"LIMIT","message":"retry"}`},
				{method: method, status: 200, body: body},
			}
			if method != "DELETE" {
				calls = append(calls, dataAPITestCall{method: "GET", status: 200, body: dataAPIGetBody})
			}
			r := dataAPIConfiguredResource(t, calls...)
			state := dataAPIState(t, nil)
			switch method {
			case "POST":
				resp := resource.CreateResponse{State: state}
				r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			case "PATCH":
				resp := resource.UpdateResponse{State: state}
				r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(state), State: state}, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			case "DELETE":
				resp := resource.DeleteResponse{State: state}
				r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			}
		})
	}
}

func TestDataAPIRefreshEverySettingsType(t *testing.T) {
	body := `{"url":"https://endpoint.test/rest/v1","status":"active","settings":{"db_aggregates_enabled":false,"db_anon_role":"anonymous","db_extra_search_path":"extensions","db_max_rows":20,"db_schemas":["public","other"],"jwt_cache_max_lifetime":15,"jwt_role_claim_key":".db_role","openapi_mode":"disabled","server_cors_allowed_origins":"https://app.test","server_timing_enabled":true},"available_schemas":["public","other","auth"]}`
	r := dataAPIConfiguredResource(t, dataAPITestCall{method: "GET", status: 200, body: body})
	settings := map[string]any{"db_aggregates_enabled": true, "db_anon_role": "guest", "db_extra_search_path": "old", "db_max_rows": float64(10), "db_schemas": []string{"public"}, "jwt_cache_max_lifetime": float64(10), "jwt_role_claim_key": ".role", "openapi_mode": "ignore-privileges", "server_cors_allowed_origins": "old", "server_timing_enabled": false}
	state := dataAPIState(t, map[string]any{"settings": settings})
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	want := dataAPIState(t, map[string]any{"settings": map[string]any{"db_aggregates_enabled": false, "db_anon_role": "anonymous", "db_extra_search_path": "extensions", "db_max_rows": float64(20), "db_schemas": []string{"public", "other"}, "jwt_cache_max_lifetime": float64(15), "jwt_role_claim_key": ".db_role", "openapi_mode": "disabled", "server_cors_allowed_origins": "https://app.test", "server_timing_enabled": true}})
	require.True(t, want.Raw.Equal(resp.State.Raw), "unexpected refreshed state")
}

func TestDataAPIProtocolLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Terraform protocol scenario is excluded from short tests")
	}
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform is required for the protocol scenario")
	}
	// Terraform can call the in-process provider concurrently during planning.
	var mu sync.Mutex
	exists := false
	var settings map[string]any
	var posts, patches int
	client, err := neon.NewClient(neon.Config{Key: "fixture-key", HTTPClient: dataAPIHTTPFixture(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := http.StatusOK
		response := map[string]any{}
		switch req.Method {
		case "POST", "PATCH":
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if req.Method == "POST" {
				exists = true
				posts++
				settings = nil
			} else {
				patches++
			}
			if incoming, ok := payload["settings"]; ok {
				settings = incoming.(map[string]any)
			}
			status = http.StatusCreated
			if req.Method == "POST" {
				response["url"] = "https://endpoint.test/rest/v1"
			}
		case "GET":
			if !exists {
				status = http.StatusNotFound
				response = map[string]any{"code": "NOT_FOUND", "message": "missing endpoint"}
			} else {
				response = map[string]any{"url": "https://endpoint.test/rest/v1", "status": "active", "settings": settings, "available_schemas": []string{"public"}}
			}
		case "DELETE":
			exists = false
		default:
			return nil, fmt.Errorf("unexpected method %s", req.Method)
		}
		body, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})})
	require.NoError(t, err)
	sdkresource.UnitTest(t, sdkresource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []sdkresource.TestStep{
			{Config: dataAPIProtocolConfig(`settings = { db_max_rows = 10 }`), Check: sdkresource.TestCheckResourceAttr("neon_data_api.this", "settings.db_max_rows", "10")},
			{Config: dataAPIProtocolConfig(`settings = { db_max_rows = 20 }`), Check: sdkresource.TestCheckResourceAttr("neon_data_api.this", "settings.db_max_rows", "20")},
			{ResourceName: "neon_data_api.this", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"settings"}},
			{Config: dataAPIProtocolConfig(``), Check: sdkresource.TestCheckNoResourceAttr("neon_data_api.this", "settings.db_max_rows")},
			{Config: dataAPIProtocolConfig(`skip_auth_schema = true`)},
		},
	})
	mu.Lock()
	defer mu.Unlock()
	require.False(t, exists, "destroy must remove remote endpoint")
	require.Equal(t, 3, posts, "settings removal and creation-only changes must replace")
	require.Equal(t, 1, patches, "ordinary settings changes must update")
}

func TestDataAPIDatabaseNamePathEscaping(t *testing.T) {
	r := dataAPITestResource(t)
	calls := 0
	client, err := neon.NewClient(neon.Config{Key: "fixture-key", HTTPClient: dataAPIHTTPFixture(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "/api/v2/projects/pr-test/branches/br-test/data-api/db%2Fwith%20slash%3Fquery%23tag", req.URL.EscapedPath())
		require.Empty(t, req.URL.RawQuery)
		require.Empty(t, req.URL.Fragment)
		body := `{}`
		if req.Method == "POST" || req.Method == "GET" {
			body = `{"url":"https://endpoint.test/rest/v1","status":"active","settings":null}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	require.NoError(t, err)
	var configured resource.ConfigureResponse
	r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError())
	state := dataAPIState(t, map[string]any{"database_name": "db/with slash?query#tag"})
	created := resource.CreateResponse{State: state}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &created)
	require.False(t, created.Diagnostics.HasError(), "%v", created.Diagnostics)
	require.Equal(t, "pr-test/br-test/db/with slash?query#tag", dataAPIString(t, created.State, "id"))
	read := resource.ReadResponse{State: created.State}
	r.Read(context.Background(), resource.ReadRequest{State: created.State}, &read)
	require.False(t, read.Diagnostics.HasError(), "%v", read.Diagnostics)
	updated := resource.UpdateResponse{State: read.State}
	r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(read.State), State: read.State}, &updated)
	require.False(t, updated.Diagnostics.HasError(), "%v", updated.Diagnostics)
	deleted := resource.DeleteResponse{State: updated.State}
	r.Delete(context.Background(), resource.DeleteRequest{State: updated.State}, &deleted)
	require.False(t, deleted.Diagnostics.HasError(), "%v", deleted.Diagnostics)
	require.Equal(t, 6, calls)
}

func dataAPIProtocolConfig(options string) string {
	return fmt.Sprintf(`resource "neon_data_api" "this" {
 project_id = "pr-test"
 branch_id = "br-test"
 database_name = "neondb"
 %s
}
`, options)
}
