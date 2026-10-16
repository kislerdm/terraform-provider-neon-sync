package provider

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

const projectDataSourceAddress = "data.neon_project.this"
const projectFixturePath = "/api/v2/projects/cool-moon-42"
const projectFixtureBranchPath = projectFixturePath + "/branches/br-primary"
const projectFixturePasswordPath = projectFixtureBranchPath + "/roles/owner/reveal_password"

const projectBranchesFixture = `{
  "branches": [
    {"id": "br-default-only", "default": true},
    {"id": "br-not-primary", "primary": false},
    {"id": "br-primary", "primary": true},
    {"id": "br-later-primary", "primary": true}
  ],
  "pagination": {"next": "ignored-cursor"}
}`

const projectEndpointsFixture = `{
  "endpoints": [
    {
      "id": "ep-later", "branch_id": "br-primary", "type": "read_write",
      "host": "ep-later.example.com", "created_at": "2026-02-01T00:00:00Z"
    },
    {
      "id": "ep-other", "branch_id": "br-other", "type": "read_write",
      "host": "ep-other.example.com", "created_at": "2025-01-01T00:00:00Z"
    },
    {
      "id": "ep-disabled", "branch_id": "br-primary", "type": "read_write",
      "host": "ep-disabled.example.com", "disabled": true,
      "created_at": "2025-01-01T00:00:00Z"
    },
    {
      "id": "ep-reader", "branch_id": "br-primary", "type": "read_only",
      "host": "ep-reader.example.com", "created_at": "2025-01-01T00:00:00Z"
    },
    {
      "id": "ep-first", "branch_id": "br-primary", "type": "read_write",
      "host": "ep-first.example.com", "created_at": "2026-01-01T00:00:00Z"
    },
    {
      "id": "ep-tied", "branch_id": "br-primary", "type": "read_write",
      "host": "ep-tied.example.com", "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}`

const projectDatabasesFixture = `{
  "databases": [
    {
      "branch_id": "br-primary", "name": "later", "owner_name": "later-owner",
      "created_at": "2026-02-01T00:00:00Z"
    },
    {
      "branch_id": "br-other", "name": "other", "owner_name": "other-owner",
      "created_at": "2025-01-01T00:00:00Z"
    },
    {
      "branch_id": "br-primary", "name": "neondb", "owner_name": "owner",
      "created_at": "2026-01-01T00:00:00Z"
    },
    {
      "branch_id": "br-primary", "name": "tied", "owner_name": "tied-owner",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}`

type projectDataSourceReply struct {
	status    int
	body      string
	err       error
	forbidden bool
}

type projectDataSourceFixture struct {
	t       *testing.T
	replies map[string]projectDataSourceReply
}

func (f projectDataSourceFixture) Do(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	require.Equal(f.t, http.MethodGet, req.Method)
	require.Empty(f.t, req.URL.RawQuery)
	reply, ok := f.replies[req.URL.Path]
	require.True(f.t, ok, "unexpected operation: %s", req.URL.Path)
	require.False(f.t, reply.forbidden, "operation must not run: %s", req.URL.Path)
	if reply.err != nil {
		return nil, reply.err
	}
	return &http.Response{
		StatusCode: reply.status,
		Body:       io.NopCloser(strings.NewReader(reply.body)),
	}, nil
}

func newProjectDataSourceClient(t *testing.T, overrides map[string]projectDataSourceReply) *neon.Client {
	t.Helper()
	replies := map[string]projectDataSourceReply{
		projectFixturePath: {
			status: http.StatusOK,
			body: `{
  "project": {"id": "cool-moon-42", "name": "My project"}
}`,
		},
		projectFixturePath + "/branches": {
			status: http.StatusOK,
			body:   projectBranchesFixture,
		},
		projectFixtureBranchPath + "/endpoints": {
			status: http.StatusOK,
			body:   projectEndpointsFixture,
		},
		projectFixtureBranchPath + "/databases": {
			status: http.StatusOK,
			body:   projectDatabasesFixture,
		},
		projectFixturePasswordPath: {
			status: http.StatusOK,
			body: `{
  "password": "fixture-password"
}`,
		},
	}
	for path, reply := range overrides {
		replies[path] = reply
	}
	client, err := neon.NewClient(neon.Config{
		Key: "test",
		HTTPClient: projectDataSourceFixture{
			t:       t,
			replies: replies,
		},
	})
	require.NoError(t, err)
	return client
}

func TestProjectDataSource(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		overrides map[string]projectDataSourceReply
		host      string
		pooler    string
		password  string
		uri       string
		pooledURI string
	}{
		{
			name:      "oldest eligible components of first primary branch",
			host:      "ep-first.example.com",
			pooler:    "ep-first-pooler.example.com",
			password:  "fixture-password",
			uri:       "postgres://owner:fixture-password@ep-first.example.com/neondb?sslmode=require",
			pooledURI: "postgres://owner:fixture-password@ep-first-pooler.example.com/neondb?sslmode=require",
		},
		{
			name: "empty password keeps empty URI fallback",
			overrides: map[string]projectDataSourceReply{
				projectFixturePasswordPath: {
					status: http.StatusOK,
					body: `{
  "password": null
}`,
				},
			},
			host:   "ep-first.example.com",
			pooler: "ep-first-pooler.example.com",
		},
		{
			name: "null host keeps empty host and URI fallbacks",
			overrides: map[string]projectDataSourceReply{
				projectFixtureBranchPath + "/endpoints": {
					status: http.StatusOK,
					body: `{
  "endpoints": [
    {"id": "ep-empty", "branch_id": "br-primary", "type": "read_write", "host": null}
  ]
}`,
				},
			},
			password: "fixture-password",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newProjectDataSourceClient(t, tt.overrides)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{
						Config: projectDataSourceConfig("cool-moon-42"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(projectDataSourceAddress, "id", "cool-moon-42"),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "name", "My project"),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "default_branch_id", "br-primary"),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "database_name", "neondb"),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "database_user", "owner"),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "database_host", tt.host),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "database_host_pooler", tt.pooler),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "database_password", tt.password),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "connection_uri", tt.uri),
							resource.TestCheckResourceAttr(projectDataSourceAddress, "connection_uri_pooler", tt.pooledURI),
						),
					},
				},
			})
		})
	}
}

func TestProjectDataSourceErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		path    string
		status  int
		message string
	}{
		{
			name:    "missing project",
			path:    "/api/v2/projects/missing-project",
			status:  http.StatusNotFound,
			message: "project not found",
		},
		{
			name:    "branch listing forbidden",
			path:    projectFixturePath + "/branches",
			status:  http.StatusForbidden,
			message: "branches unavailable",
		},
		{
			name:    "selected branch missing",
			path:    projectFixtureBranchPath + "/endpoints",
			status:  http.StatusNotFound,
			message: "branch not found",
		},
		{
			name:    "database lookup rejected",
			path:    projectFixtureBranchPath + "/databases",
			status:  http.StatusBadRequest,
			message: "database lookup rejected",
		},
		{
			name:    "password access denied",
			path:    projectFixturePasswordPath,
			status:  http.StatusPreconditionFailed,
			message: "password access unavailable",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newProjectDataSourceClient(t, map[string]projectDataSourceReply{
				tt.path: {
					status: tt.status,
					body: fmt.Sprintf(`{
  "code": "FIXTURE_ERROR",
  "message": %q
}`, tt.message),
				},
			})
			projectID := "cool-moon-42"
			if tt.name == "missing project" {
				projectID = "missing-project"
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{
						Config: projectDataSourceConfig(projectID),
						ExpectError: regexp.MustCompile(regexp.QuoteMeta(fmt.Sprintf(
							"[HTTP Code: %d][Error Code: FIXTURE_ERROR] %s", tt.status, tt.message,
						))),
					},
				},
			})
		})
	}
}

func TestProjectDataSourceMissingComponents(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		path      string
		body      string
		component string
	}{
		{
			name:      "no branches",
			path:      projectFixturePath + "/branches",
			body:      `{"branches": []}`,
			component: "primary branch",
		},
		{
			name: "default does not replace null or false primary",
			path: projectFixturePath + "/branches",
			body: `{
  "branches": [
    {"id": "br-default", "primary": null, "default": true},
    {"id": "br-false", "primary": false}
  ]
}`,
			component: "primary branch",
		},
		{
			name:      "no endpoints",
			path:      projectFixtureBranchPath + "/endpoints",
			body:      `{"endpoints": null}`,
			component: "endpoint",
		},
		{
			name: "only ineligible endpoints",
			path: projectFixtureBranchPath + "/endpoints",
			body: `{
  "endpoints": [
    {"branch_id": "br-other", "type": "read_write"},
    {"branch_id": "br-primary", "type": "read_only"},
    {"branch_id": "br-primary", "type": "read_write", "disabled": true}
  ]
}`,
			component: "endpoint",
		},
		{
			name:      "no databases",
			path:      projectFixtureBranchPath + "/databases",
			body:      `{"databases": []}`,
			component: "database",
		},
		{
			name: "only other branch databases",
			path: projectFixtureBranchPath + "/databases",
			body: `{
  "databases": [{"branch_id": "br-other", "name": "other", "owner_name": "other-owner"}]
}`,
			component: "database",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newProjectDataSourceClient(t, map[string]projectDataSourceReply{
				tt.path: {
					status: http.StatusOK,
					body:   tt.body,
				},
				projectFixturePasswordPath: {
					forbidden: true,
				},
			})
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{
						Config:      projectDataSourceConfig("cool-moon-42"),
						ExpectError: regexp.MustCompile("No .*" + tt.component),
					},
				},
			})
		})
	}
}

func TestProjectDataSourceReadFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		reply projectDataSourceReply
		want  string
	}{
		{
			name: "transport",
			reply: projectDataSourceReply{
				err: errors.New("fixture transport failed"),
			},
			want: "fixture transport failed",
		},
		{
			name: "malformed response",
			reply: projectDataSourceReply{
				status: http.StatusOK,
				body:   `not-json`,
			},
			want: "invalid character",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newProjectDataSourceClient(t, map[string]projectDataSourceReply{
				projectFixturePath: tt.reply,
			})
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{
						Config:      projectDataSourceConfig("cool-moon-42"),
						ExpectError: regexp.MustCompile(tt.want),
					},
				},
			})
		})
	}
}

func TestProjectDataSourceConfiguredOutput(t *testing.T) {
	t.Parallel()
	client := newProjectDataSourceClient(t, nil)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resource.TestStep{
			{
				Config: `
data "neon_project" "this" {
  id   = "cool-moon-42"
  name = "configured-output"
}
`,
				ExpectError: regexp.MustCompile(`(?i)(computed|unconfigurable|read.only)`),
			},
		},
	})
}

func projectDataSourceConfig(projectID string) string {
	return fmt.Sprintf(`
data "neon_project" "this" {
  id = %q
}
`, projectID)
}
