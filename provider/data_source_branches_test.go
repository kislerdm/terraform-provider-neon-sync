package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-mux/tf5to6server"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

const branchesAddress = "data.neon_branches.this"

const branchesOneResponse = `{
  "branches": [
    {
      "id": "br-first-123",
      "project_id": "response-project",
      "name": "main",
      "parent_id": "br-parent-789",
      "logical_size": 1048577,
      "primary": true,
      "default": true,
      "protected": false,
      "current_state": "ready",
      "state_changed_at": "2026-01-01T00:00:00Z",
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "cpu_used_sec": 0,
      "active_time_seconds": 0,
      "compute_time_seconds": 0,
      "written_data_bytes": 0,
      "data_transfer_bytes": 0
    }
  ]
}`

const branchesTwoResponse = `{
  "branches": [
    {
      "id": "br-first-123",
      "project_id": "response-project",
      "name": "first",
      "parent_id": "",
      "logical_size": 0,
      "primary": false,
      "default": false,
      "protected": false,
      "current_state": "ready",
      "state_changed_at": "2026-01-01T00:00:00Z",
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "cpu_used_sec": 0,
      "active_time_seconds": 0,
      "compute_time_seconds": 0,
      "written_data_bytes": 0,
      "data_transfer_bytes": 0
    },
    {
      "id": "br-second-456",
      "project_id": "response-project",
      "name": "second",
      "default": true,
      "protected": false,
      "current_state": "ready",
      "state_changed_at": "2026-01-01T00:00:00Z",
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "cpu_used_sec": 0,
      "active_time_seconds": 0,
      "compute_time_seconds": 0,
      "written_data_bytes": 0,
      "data_transfer_bytes": 0
    }
  ],
  "pagination": {
    "next": "next-page",
    "sort_by": "updated_at",
    "sort_order": "desc"
  }
}`

func TestBranchesDataSource(t *testing.T) {
	t.Parallel()
	testBranchesDataSource(t, false)
}

func TestBranchesDataSourceLegacy(t *testing.T) {
	t.Parallel()
	testBranchesDataSource(t, true)
}

func testBranchesDataSource(t *testing.T, legacy bool) {
	t.Helper()
	for _, tt := range []struct {
		name      string
		projectID string
		status    int
		bodies    []string
		check     resource.TestCheckFunc
		wantError string
	}{
		{
			name:      "missing project",
			projectID: "missing-project",
			status:    http.StatusNotFound,
			bodies: []string{`{
  "code": "NOT_FOUND",
  "message": "project not found"
}`},
			wantError: "[HTTP Code: 404][Error Code: NOT_FOUND] project not found",
		},
		{
			name:      "one branch",
			projectID: "cool-moon-42",
			status:    http.StatusOK,
			bodies:    []string{branchesOneResponse},
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", "1",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.id", "br-first-123",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.name", "main",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_id", "br-parent-789",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.logical_size", "1048577",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.primary", "true",
				),
			),
		},
		{
			name:      "two branches",
			projectID: "cool-moon-42",
			status:    http.StatusOK,
			bodies: []string{
				branchesTwoResponse,
				// JSON null and omission decode identically in the pinned SDK pointers.
				regexp.MustCompile(`"name": "second",`).ReplaceAllString(branchesTwoResponse, `"name": "second",
      "parent_id": null,
      "logical_size": null,
      "primary": null,`),
			},
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", "2",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.id", "br-first-123",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.name", "first",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_id", "",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.logical_size", "0",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.primary", "false",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.id", "br-second-456",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.name", "second",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.parent_id", "",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.logical_size", "0",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.primary", "false",
				),
			),
		},
		{
			name:      "no branches",
			projectID: "cool-moon-42",
			status:    http.StatusOK,
			bodies: []string{
				`{
  "branches": []
}`,
				`{
  "branches": null
}`,
				`{}`,
			},
			check: resource.TestCheckResourceAttr(
				branchesAddress, "branches.#", "0",
			),
		},
	} {
		if legacy && tt.wantError != "" {
			continue // The legacy reader discards SDK errors; only successful output is compatible.
		}
		t.Run(tt.name, func(t *testing.T) {
			for i, body := range tt.bodies {
				t.Run(fmt.Sprintf("response %d", i+1), func(t *testing.T) {
					client := newBranchesTestClient(t, tt.projectID, tt.status, body)
					factories := newUnitTestProviderFactories(&providerAdapter{sdk: client})
					if legacy {
						factories = legacyBranchesFactories(client)
					}
					step := resource.TestStep{
						Config: branchesDataSourceConfig(tt.projectID),
					}
					if tt.wantError != "" {
						step.ExpectError = regexp.MustCompile(regexp.QuoteMeta(tt.wantError))
					} else {
						step.Check = resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(
								branchesAddress, "project_id", "cool-moon-42",
							),
							resource.TestCheckResourceAttr(
								branchesAddress, "id", "cool-moon-42/branches",
							),
							tt.check,
						)
					}
					resource.UnitTest(t, resource.TestCase{
						ProtoV6ProviderFactories: factories,
						Steps:                    []resource.TestStep{step},
					})
				})
			}
		})
	}
}

func TestBranchesDataSourceConfiguredOutputs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		output string
	}{
		{
			name:   "id",
			output: `id = "configured-id"`,
		},
		{
			name:   "branches assignment",
			output: `branches = []`,
		},
		{
			name:   "branches block",
			output: `branches { name = "configured-name" }`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, legacy := range []bool{true, false} {
				t.Run(fmt.Sprintf("legacy %t", legacy), func(t *testing.T) {
					client := newBranchesTestClient(t, "cool-moon-42", http.StatusOK, branchesOneResponse)
					factories := newUnitTestProviderFactories(&providerAdapter{sdk: client})
					if legacy {
						factories = legacyBranchesFactories(client)
					}
					resource.UnitTest(t, resource.TestCase{
						ProtoV6ProviderFactories: factories,
						Steps: []resource.TestStep{
							{
								Config: fmt.Sprintf(`
data "neon_branches" "this" {
  project_id = "cool-moon-42"
  %s
}
`, tt.output),
								ExpectError: regexp.MustCompile(`(?i)(computed|unconfigurable|read.only|not expected|invalid or unknown key)`),
							},
						},
					})
				})
			}
		})
	}
}

func TestBranchesDataSourceCompatibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newBranchesTestClient(t, "cool-moon-42", http.StatusOK, branchesTwoResponse)
	legacyServer, err := legacyBranchesFactories(client)["neon"]()
	require.NoError(t, err)
	legacySchema, err := legacyServer.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	require.Empty(t, legacySchema.Diagnostics)

	frameworkServer, err := newUnitTestProviderFactories(&providerAdapter{sdk: client})["neon"]()
	require.NoError(t, err)
	frameworkSchema, err := frameworkServer.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	require.Empty(t, frameworkSchema.Diagnostics)
	oldSchema := legacySchema.DataSourceSchemas["neon_branches"]
	newSchema := frameworkSchema.DataSourceSchemas["neon_branches"]
	require.NotNil(t, newSchema, "the migrated provider must read the legacy branch-list contract")
	require.True(t, oldSchema.ValueType().Equal(newSchema.ValueType()), "legacy list/object types must remain readable")

	for _, dataSchema := range []*tfprotov6.Schema{oldSchema, newSchema} {
		for _, attribute := range dataSchema.Block.Attributes {
			require.False(t, attribute.Sensitive, attribute.Name)
			if attribute.Name == "project_id" {
				require.True(t, attribute.Required)
				require.False(t, attribute.Computed)
			} else {
				require.True(t, attribute.Computed, attribute.Name)
				require.False(t, attribute.Required, attribute.Name)
			}
		}
	}
	for _, attribute := range newSchema.Block.Attributes {
		require.False(t, attribute.Optional, attribute.Name)
		if attribute.Name == "branches" {
			for _, field := range attribute.NestedType.Attributes {
				require.True(t, field.Computed, field.Name)
				require.Equal(t, field.Name == "name" || field.Name == "parent_id", field.Optional, field.Name)
			}
		}
	}
	for name, field := range legacyBranchesSchema().Schema {
		require.Nil(t, field.Default, name)
		require.Nil(t, field.ValidateFunc, name)
		require.Nil(t, field.ValidateDiagFunc, name)
		require.False(t, field.ForceNew, name)
	}

	for _, provider := range []struct {
		server tfprotov6.ProviderServer
		schema *tfprotov6.Schema
	}{
		{
			server: legacyServer,
			schema: legacySchema.Provider,
		},
		{
			server: frameworkServer,
			schema: frameworkSchema.Provider,
		},
	} {
		config, err := tfprotov6.NewDynamicValue(
			provider.schema.ValueType(),
			tftypes.NewValue(provider.schema.ValueType(), map[string]tftypes.Value{
				"api_key": tftypes.NewValue(tftypes.String, "test"),
			}),
		)
		require.NoError(t, err)
		configured, err := provider.server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{
			TerraformVersion: "1.6.0",
			Config:           &config,
		})
		require.NoError(t, err)
		require.Empty(t, configured.Diagnostics)
	}

	valueType := oldSchema.ValueType()
	config, err := tfprotov6.NewDynamicValue(
		valueType,
		tftypes.NewValue(valueType, map[string]tftypes.Value{
			"id":         tftypes.NewValue(tftypes.String, nil),
			"project_id": tftypes.NewValue(tftypes.String, "cool-moon-42"),
			"branches":   tftypes.NewValue(valueType.(tftypes.Object).AttributeTypes["branches"], nil),
		}),
	)
	require.NoError(t, err)
	legacyRead, err := legacyServer.ReadDataSource(ctx, &tfprotov6.ReadDataSourceRequest{
		TypeName: "neon_branches",
		Config:   &config,
	})
	require.NoError(t, err)
	require.Empty(t, legacyRead.Diagnostics)
	decoded, err := legacyRead.State.Unmarshal(newSchema.ValueType())
	require.NoError(t, err)
	frameworkRead, err := frameworkServer.ReadDataSource(ctx, &tfprotov6.ReadDataSourceRequest{
		TypeName: "neon_branches",
		Config:   &config,
	})
	require.NoError(t, err)
	require.Empty(t, frameworkRead.Diagnostics)
	refreshed, err := frameworkRead.State.Unmarshal(newSchema.ValueType())
	require.NoError(t, err)
	require.True(t,
		decoded.Equal(refreshed),
		"refresh must preserve the actual legacy identity, list order, and mapped values",
	)
}

func branchesDataSourceConfig(projectID string) string {
	return fmt.Sprintf(`
data "neon_branches" "this" {
  project_id = %q
}
`, projectID)
}

type branchesHTTPFixture struct {
	branchDataSourceHTTPFixture
}

func (f branchesHTTPFixture) Do(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	require.Empty(f.t, req.URL.RawQuery, "listing must keep all optional parameters unset")
	return f.branchDataSourceHTTPFixture.Do(req)
}

func newBranchesTestClient(t *testing.T, projectID string, status int, body string) *neon.Client {
	t.Helper()
	client, err := neon.NewClient(neon.Config{
		Key: "test",
		HTTPClient: branchesHTTPFixture{
			branchDataSourceHTTPFixture: branchDataSourceHTTPFixture{
				t:      t,
				path:   "/api/v2/projects/" + projectID + "/branches",
				status: status,
				body:   body,
			},
		},
	})
	require.NoError(t, err)
	return client
}

func legacyBranchesFactories(client *neon.Client) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"neon": func() (tfprotov6.ProviderServer, error) {
			p := New("legacyBranchesTest")
			p.DataSourcesMap = map[string]*schema.Resource{
				"neon_branches": legacyBranchesSchema(),
			}
			p.ConfigureContextFunc = func(context.Context, *schema.ResourceData) (interface{}, diag.Diagnostics) {
				return client, nil
			}
			return tf5to6server.UpgradeServer(context.Background(), func() tfprotov5.ProviderServer {
				return p.GRPCProvider()
			})
		},
	}
}
