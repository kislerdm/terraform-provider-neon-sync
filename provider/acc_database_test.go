package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func newDatabaseConfig(projectName, databaseName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {name = %q}
resource "neon_database" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.default_branch_id
	owner_name = neon_project.this.database_user
	name       = %q
}`, projectName, databaseName)
}

func TestRecreateDatabaseIfNotFound(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "databaseRecreation-"
	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	var preConfig = func(projectName, dbName string) int64 {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}
		br, err := client.ListProjectBranches(ref.ID, nil, nil, nil, nil, nil, nil)
		if err != nil {
			panic(err)
		}
		var dbID int64
		for _, branch := range br.Branches {
			if branch.Default {
				resp, err := client.GetProjectBranchDatabase(ref.ID, branch.ID, dbName)
				if err != nil {
					panic(err)
				}
				dbID = resp.Database.ID
				op, err := client.DeleteProjectBranchDatabase(ref.ID, branch.ID, dbName)
				if err != nil {
					panic(err)
				}
				waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
			}
		}
		return dbID
	}

	t.Run("shall yield non empty refresh plan if the database was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newDatabaseConfig(projectName, "test")
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_database.this", "name", "test"),
						func(state *terraform.State) error {
							database, ok := state.RootModule().Resources["neon_database.this"]
							if !ok {
								return fmt.Errorf("resource neon_database.this not found in state")
							}
							assert.NotEmpty(t, database.Primary.ID)
							assert.Equal(t, "test", database.Primary.Attributes["name"])
							return nil
						},
					),
				},
				{
					PreConfig:    func() { preConfig(projectName, "test") },
					RefreshState: true, ExpectNonEmptyPlan: true,
				},
			},
		})
	})

	t.Run("shall destroy even if the database was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newDatabaseConfig(projectName, "test")
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: config},
				{
					Config:    config,
					PreConfig: func() { preConfig(projectName, "test") },
					Destroy:   true,
					Check: func(state *terraform.State) error {
						_, ok := state.RootModule().Resources["neon_database.this"]
						assert.False(t, ok, "resource neon_database.this should be destroyed")
						return nil
					},
				},
			},
		})
	})

	t.Run("shall recreate database upon update if it was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var refDatabaseID int64
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: newDatabaseConfig(projectName, "foo")},
				{
					Config:    newDatabaseConfig(projectName, "bar"),
					PreConfig: func() { refDatabaseID = preConfig(projectName, "foo") },
					Check: func(state *terraform.State) error {
						database, ok := state.RootModule().Resources["neon_database.this"]
						if !ok {
							return fmt.Errorf("resource neon_database.this not found in state")
						}
						assert.Equal(t, "bar", database.Primary.Attributes["name"])
						assert.NotEqual(t, refDatabaseID, database.Primary.ID)
						return nil
					},
				},
			},
		})
	})

	t.Run("shall fail to import database if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := newDatabaseConfig(projectName, "test")
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: config},
				{
					Config: config, ImportState: true, ResourceName: "neon_database.this",
					ImportStateIdFunc: func(s *terraform.State) (string, error) {
						database := s.RootModule().Resources["neon_database.this"]
						if database == nil {
							return "", fmt.Errorf("resource neon_database.this not found in state")
						}
						projectID := database.Primary.Attributes["project_id"]
						branchID := database.Primary.Attributes["branch_id"]
						name := database.Primary.Attributes["name"]
						op, err := client.DeleteProjectBranchDatabase(projectID, branchID, name)
						if err != nil {
							return "", err
						}
						waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
						return fmt.Sprintf("%s/%s/%s", projectID, branchID, name), nil
					},
					ExpectError: regexp.MustCompile("404"),
				},
				{Config: config},
			},
		})
	})
}
