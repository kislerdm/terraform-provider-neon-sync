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

func newRoleConfig(projectName, roleName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {name = %q}
resource "neon_role" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.default_branch_id
  name       = %q
}`, projectName, roleName)
}

func newRoleProjectConfig(projectName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" { name = %q }`, projectName)
}

func TestRecreateRoleIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "roleRecreation"

	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	var preConfig = func(projectName string, roleName string) {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}

		respBranches, err := client.ListProjectBranches(ref.ID,
			nil, nil, nil, nil, nil, nil)
		if err != nil {
			panic(err)
		}

		var branchID string
		for _, branch := range respBranches.Branches {
			if branch.Default {
				branchID = branch.ID
			}
		}

		op, err := client.DeleteProjectBranchRole(ref.ID, branchID, roleName)
		if err != nil {
			panic(err)
		}
		waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
	}

	t.Run("shall indicate non empty plan if the role was deleted outside of terraform", func(t *testing.T) {
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
						Config: newRoleConfig(projectName, "test"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
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

	t.Run("shall destroy even if the role was deleted outside of terraform,", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newRoleConfig(projectName, "test")
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
								"neon_role.this",
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
							_, ok := s.RootModule().Resources["neon_role.this"]
							assert.False(t, ok, "resource neon_role.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate role upon update if it was deleted outside of terraform", func(t *testing.T) {
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
						Config: newRoleConfig(projectName, "foo"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
								"name", "foo",
							),
						),
					},
					{
						Config: newRoleConfig(projectName, "bar"),
						PreConfig: func() {
							preConfig(projectName, "foo")
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
								"name", "bar",
							),
							func(state *terraform.State) error {
								project := state.RootModule().Resources["neon_project.this"]
								role := state.RootModule().Resources["neon_role.this"]
								if project == nil || role == nil {
									return fmt.Errorf("expected project and role resources in state")
								}
								projectID := project.Primary.Attributes["id"]
								branchID := role.Primary.Attributes["branch_id"]
								if projectID == "" || branchID == "" {
									return fmt.Errorf("state is missing project or branch ID")
								}

								respRoles, err := client.ListProjectBranchRoles(projectID, branchID)
								if err != nil {
									return err
								}

								var found bool
								var oldFound bool
								for _, role := range respRoles.Roles {
									if role.Name == "bar" {
										found = true
									}
									if role.Name == "foo" {
										oldFound = true
									}
								}
								assert.Truef(t, found, "role 'bar' is expected to be found after recreation")
								assert.Falsef(t, oldFound, "role 'foo' is not expected to be found")
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import role if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newRoleConfig(projectName, "test")
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
						ResourceName: "neon_role.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							project := s.RootModule().Resources["neon_project.this"]
							role := s.RootModule().Resources["neon_role.this"]
							if project == nil || role == nil {
								return "", fmt.Errorf("expected project and role resources in state")
							}
							projectID := project.Primary.Attributes["id"]
							branchID := role.Primary.Attributes["branch_id"]
							if projectID == "" || branchID == "" {
								return "", fmt.Errorf("state is missing project or branch ID")
							}

							op, err := client.DeleteProjectBranchRole(projectID, branchID, "test")
							if err != nil {
								return "", err
							}
							waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)

							return fmt.Sprintf("%s/%s/test", projectID, branchID), nil
						},
						ExpectError: regexp.MustCompile("404"),
					},
					// to avoid dangling resources on post-test destroy
					{
						Config: newRoleProjectConfig(projectName),
					},
				},
			})
	})
}
