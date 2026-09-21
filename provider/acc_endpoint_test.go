package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func newEndpointConfig(projectName, endpointType string) string {
	return fmt.Sprintf(`resource "neon_project" "this" { name = %q }
resource "neon_endpoint" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.default_branch_id
	type       = %q
}`, projectName, endpointType)
}

func newNamedEndpointConfig(projectName, endpointType, projectNameCompute, endpointName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
	name = %q
	primary_compute {
		name = %q
	}
}
resource "neon_endpoint" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.default_branch_id
	type       = %q
	name       = %q
}`, projectName, projectNameCompute, endpointType, endpointName)
}

func TestRecreateEndpointIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "endpointRecreation"

	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	var preConfig = func(projectName string) {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}

		resp, err := client.ListProjectEndpoints(ref.ID)
		if err != nil {
			panic(err)
		}
		for _, endpoint := range resp.Endpoints {
			if endpoint.Type == endpointTypeReadOnly {
				op, err := client.DeleteProjectEndpoint(ref.ID, endpoint.ID)
				if err != nil {
					panic(err)
				}
				waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
			}
		}
	}

	t.Run("shall indicate non empty plan if the endpoint was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"neon": func() (*schema.Provider, error) {
						return newAccTest(), nil
					},
				},
				Steps: []resource.TestStep{
					{
						Config: newEndpointConfig(projectName, endpointTypeReadOnly.String()),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_endpoint.this",
								"type", endpointTypeReadOnly.String(),
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName)
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the endpoint was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newEndpointConfig(projectName, endpointTypeReadOnly.String())
		resource.Test(
			t, resource.TestCase{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"neon": func() (*schema.Provider, error) {
						return newAccTest(), nil
					},
				},
				Steps: []resource.TestStep{
					{
						Config: config,
					},
					{
						PreConfig: func() {
							preConfig(projectName)
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_endpoint.this"]
							assert.False(t, ok, "resource neon_endpoint.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate endpoint upon update if it was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"neon": func() (*schema.Provider, error) {
						return newAccTest(), nil
					},
				},
				Steps: []resource.TestStep{
					{
						Config: newEndpointConfig(projectName, endpointTypeReadOnly.String()) + `

resource "neon_endpoint" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.default_branch_id
	type       = "read_only"
	disabled   = false
}`,
						Check: resource.TestCheckResourceAttr(
							"neon_endpoint.this",
							"disabled", "false",
						),
					},
					{
						Config: newEndpointConfig(projectName, endpointTypeReadOnly.String()) + `

resource "neon_endpoint" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.default_branch_id
	type       = "read_only"
	disabled   = true
}`,
						PreConfig: func() {
							preConfig(projectName)
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_endpoint.this",
								"disabled", "true",
							),
							func(state *terraform.State) error {
								endpoint := state.RootModule().Resources["neon_endpoint.this"]
								assert.Equal(t, "true", endpoint.Primary.Attributes["disabled"])
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import endpoint if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newEndpointConfig(projectName, endpointTypeReadOnly.String())
		resource.Test(
			t, resource.TestCase{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"neon": func() (*schema.Provider, error) {
						return newAccTest(), nil
					},
				},
				Steps: []resource.TestStep{
					{
						Config: config,
					},
					{
						Config:       config,
						ImportState:  true,
						ResourceName: "neon_endpoint.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							project, ok := s.RootModule().Resources["neon_project.this"]
							if !ok {
								return "", fmt.Errorf("resource neon_project.this not found in state")
							}
							endpoint, ok := s.RootModule().Resources["neon_endpoint.this"]
							if !ok {
								return "", fmt.Errorf("resource neon_endpoint.this not found in state")
							}
							projectID := project.Primary.ID
							endpointID := endpoint.Primary.ID
							if projectID == "" || endpointID == "" {
								return "", fmt.Errorf("state is missing project or endpoint ID")
							}
							op, err := client.DeleteProjectEndpoint(projectID, endpointID)
							if err != nil {
								return "", err
							}
							waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
							return fmt.Sprintf("%s/%s", projectID, endpointID), nil
						},
						ExpectError: regexp.MustCompile("404"),
					},
					// to avoid dangling resources on post-test destroy
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" { name = %q }`, projectName),
					},
				},
			})
	})
}

func TestEndpointName(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "endpointName"

	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	projectName := newProjectName(projectNamePrefix)
	resource.Test(
		t, resource.TestCase{
			ProviderFactories: map[string]func() (*schema.Provider, error){
				"neon": func() (*schema.Provider, error) {
					return newAccTest(), nil
				},
			},
			Steps: []resource.TestStep{
				{
					Config: newNamedEndpointConfig(projectName, "read_only", "foo", "bar"),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(
							"neon_project.this",
							"primary_compute.0.name", "foo",
						),
						resource.TestCheckResourceAttr(
							"neon_endpoint.this",
							"name", "bar",
						),
						func(state *terraform.State) error {
							project := state.RootModule().Resources["neon_project.this"]
							endpoint := state.RootModule().Resources["neon_endpoint.this"]
							assert.Equal(t, project.Primary.ID, endpoint.Primary.Attributes["project_id"])
							assert.Equal(t, "bar", endpoint.Primary.Attributes["name"])
							return nil
						},
					),
				},
				{
					Config: newEndpointConfig(projectName, "read_only"),
					Check: func(state *terraform.State) error {
						endpoint := state.RootModule().Resources["neon_endpoint.this"]
						assert.Equal(t, "", endpoint.Primary.Attributes["name"])
						return nil
					},
				},
			},
		})
}
