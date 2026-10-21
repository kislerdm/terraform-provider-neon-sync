package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
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
      "protected": true,
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
  ]
}`

func TestBranchesDataSource(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                    string
		projectID               string
		status                  int
		bodies                  []string
		check                   resource.TestCheckFunc
		wantError               string
		checkPrimaryDeprecation bool
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
			name:                    "one branch",
			projectID:               "cool-moon-42",
			status:                  http.StatusOK,
			bodies:                  []string{branchesOneResponse},
			checkPrimaryDeprecation: true,
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
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.default", "true",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.protected", "true",
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
					branchesAddress, "branches.0.default", "false",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.protected", "false",
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
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.default", "true",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.protected", "false",
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
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", "0",
				),
				resource.TestCheckOutput("branch_count", "0"),
			),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for i, body := range tt.bodies {
				t.Run(fmt.Sprintf("response %d", i+1), func(t *testing.T) {
					client := newBranchesTestClient(t, tt.projectID, tt.status, body)
					factories := newUnitTestProviderFactories(&providerAdapter{sdk: client})
					if tt.checkPrimaryDeprecation {
						server, err := factories["neon"]()
						require.NoError(t, err)
						response, err := server.GetProviderSchema(
							context.Background(),
							&tfprotov6.GetProviderSchemaRequest{},
						)
						require.NoError(t, err)
						require.Empty(t, response.Diagnostics)
						var primary *tfprotov6.SchemaAttribute
						for _, attribute := range response.DataSourceSchemas["neon_branches"].Block.Attributes {
							if attribute.Name != "branches" {
								continue
							}
							for _, field := range attribute.NestedType.Attributes {
								require.True(t, field.Computed, field.Name)
								require.False(t, field.Optional, field.Name)
								require.False(t, field.Required, field.Name)
								if field.Name == "primary" {
									primary = field
								}
							}
						}
						require.NotNil(t, primary)
						require.True(t, primary.Deprecated)
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
								branchesAddress, "id", "cool-moon-42",
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

func TestBranchesDataSourcePagination(t *testing.T) {
	t.Parallel()
	firstPage := strings.TrimSuffix(branchesTwoResponse, "\n}") + `,
  "pagination": {
    "next": "next+page/with=cursor&value",
    "sort_by": "updated_at",
    "sort_order": "desc"
  }
}`
	lastPage := strings.NewReplacer(
		"br-first-123", "br-third-789",
		`"name": "main"`, `"name": "third"`,
		`"primary": true`, `"primary": false`,
		`"default": true`, `"default": false`,
	).Replace(branchesOneResponse)
	middlePage := strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": {
    "next": "last-page"
  }
}`
	nextQuery := url.Values{
		"cursor":     {"next+page/with=cursor&value"},
		"sort_by":    {"updated_at"},
		"sort_order": {"desc"},
	}
	lastQuery := url.Values{
		"cursor":     {"last-page"},
		"sort_by":    {"updated_at"},
		"sort_order": {"desc"},
	}
	for _, tt := range []struct {
		name      string
		pages     []branchesPage
		wantError string
	}{
		{
			name: "three pages retain order and sorting",
			pages: []branchesPage{
				{
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body:   middlePage,
				},
				{
					query:  lastQuery,
					status: http.StatusOK,
					body: `{
  "branches": []
}`,
				},
			},
		},
		{
			name: "later page retry adds no duplicate branches",
			pages: []branchesPage{
				{
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusTooManyRequests,
					body: `{
  "code": "RATE_LIMITED",
  "message": "retry this page"
}`,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body:   lastPage,
				},
			},
		},
		{
			name: "later page failure rejects partial results",
			pages: []branchesPage{
				{
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusForbidden,
					body: `{
  "code": "FORBIDDEN",
  "message": "page access denied"
}`,
				},
			},
			wantError: "[HTTP Code: 403][Error Code: FORBIDDEN] page access denied",
		},
		{
			name: "null pagination ends traversal",
			pages: []branchesPage{
				{
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body: strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": null
}`,
				},
			},
		},
		{
			name: "absent next cursor ends traversal",
			pages: []branchesPage{
				{
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body: strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": {}
}`,
				},
			},
		},
		{
			name: "empty next cursor ends traversal",
			pages: []branchesPage{
				{
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body: strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": {
    "next": ""
  }
}`,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &branchesPaginationHTTPFixture{
				t:     t,
				pages: tt.pages,
			}
			client, err := neon.NewClient(neon.Config{
				Key:        "test",
				HTTPClient: fixture,
			})
			require.NoError(t, err)
			step := resource.TestStep{
				Config: branchesDataSourceConfig("cool-moon-42"),
			}
			if tt.wantError != "" {
				step.ExpectError = regexp.MustCompile(regexp.QuoteMeta(tt.wantError))
			} else {
				step.Check = resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						branchesAddress, "id", "cool-moon-42",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "project_id", "cool-moon-42",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.#", "3",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.0.id", "br-first-123",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.1.id", "br-second-456",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.id", "br-third-789",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.name", "third",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.logical_size", "1048577",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.primary", "false",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.default", "false",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.protected", "true",
					),
					resource.TestCheckOutput("branch_count", "3"),
				)
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps:                    []resource.TestStep{step},
			})
			fixture.mu.Lock()
			requests := fixture.requests
			fixture.mu.Unlock()
			require.Positive(t, requests)
			require.Zero(t, requests%len(tt.pages), "each read must finish its expected page sequence")
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
		{
			name:   "unsupported name input",
			output: `name = "configured-name"`,
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

output "branch_count" {
  value = length(data.neon_branches.this.branches)
}
`, projectID)
}

type branchesPage struct {
	query  url.Values
	status int
	body   string
}

type branchesPaginationHTTPFixture struct {
	t        *testing.T
	pages    []branchesPage
	mu       sync.Mutex
	requests int
}

func (f *branchesPaginationHTTPFixture) Do(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	page := f.pages[f.requests%len(f.pages)]
	f.requests++
	query := page.query
	if query == nil {
		query = url.Values{}
	}
	if !reflect.DeepEqual(query, req.URL.Query()) {
		return nil, fmt.Errorf("unexpected branches query: got %v, want %v", req.URL.Query(), query)
	}
	return branchDataSourceHTTPFixture{
		t:      f.t,
		path:   "/api/v2/projects/cool-moon-42/branches",
		status: page.status,
		body:   page.body,
	}.Do(req)
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
