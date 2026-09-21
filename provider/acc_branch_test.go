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

func branchConfig(projectName, branchName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" { name = %q }
resource "neon_branch" "this" {
	project_id = neon_project.this.id
	name       = %q
}`, projectName, branchName)
}

func TestRecreateBranchIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "branchRecreation-"

	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	var preConfig = func(projectName string, branchName string) {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}

		resp, err := client.ListProjectBranches(ref.ID,
			nil, nil, nil, nil, nil, nil)
		if err != nil {
			panic(err)
		}
		for _, branch := range resp.Branches {
			if branch.Name == branchName {
				op, err := client.DeleteProjectBranch(ref.ID, branch.ID)
				if err != nil {
					panic(err)
				}
				waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
			}
		}
	}

	t.Run("shall indicate non empty plan if the branch was deleted outside of terraform", func(t *testing.T) {
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
						Config: branchConfig(projectName, "test"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "test",
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the branch was deleted outside of terraform,", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := branchConfig(projectName, "test")
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
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "test",
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_branch.this"]
							assert.False(t, ok, "resource neon_branch.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate branch upon update if it was deleted outside of terraform", func(t *testing.T) {
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
						Config: branchConfig(projectName, "foo"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "foo",
							),
						),
					},
					{
						Config: branchConfig(projectName, "bar"),
						PreConfig: func() {
							preConfig(projectName, "foo")
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "bar",
							),
							func(state *terraform.State) error {
								branch, ok := state.RootModule().Resources["neon_branch.this"]
								if !ok {
									return fmt.Errorf("resource neon_branch.this not found in state")
								}
								assert.Equal(t, "bar", branch.Primary.Attributes["name"])
								assert.NotEmpty(t, branch.Primary.Attributes["id"])
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import branch if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := branchConfig(projectName, "test")
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
						ResourceName: "neon_branch.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							branch, ok := s.RootModule().Resources["neon_branch.this"]
							if !ok {
								return "", fmt.Errorf("resource neon_branch.this not found in state")
							}
							branchID := branch.Primary.Attributes["id"]
							projectID := branch.Primary.Attributes["project_id"]
							if branchID == "" || projectID == "" {
								return "", fmt.Errorf("branch state is missing id or project_id")
							}
							op, err := client.DeleteProjectBranch(projectID, branchID)
							if err != nil {
								return "", err
							}
							waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
							return fmt.Sprintf("%s/%s", projectID, branchID), nil
						},
						ExpectError: regexp.MustCompile("404"),
					},
					// to avoid dangling resources on post-test destroy
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" { name = "%s" }`, projectName),
					},
				},
			})
	})
}
