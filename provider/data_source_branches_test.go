package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
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
		t.Run(tt.name, func(t *testing.T) {
			for i, body := range tt.bodies {
				t.Run(fmt.Sprintf("response %d", i+1), func(t *testing.T) {
					client := newBranchesTestClient(t, tt.projectID, tt.status, body)
					factories := newUnitTestProviderFactories(&providerAdapter{sdk: client})
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
			client := newBranchesTestClient(t, "cool-moon-42", http.StatusOK, branchesOneResponse)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
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
