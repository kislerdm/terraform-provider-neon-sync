package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func newBackupScheduleConfig(projectName, frequency string, day, hour, retentionSeconds int) string {
	return fmt.Sprintf(`resource "neon_project" "this" {name = %q}

resource "neon_branch_backup_schedule" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.default_branch_id
  schedule = [
    {
      frequency         = %q
      day               = %d
      hour              = %d
      retention_seconds = %d
    }
  ]
}
`, projectName, frequency, day, hour, retentionSeconds)
}

func newBackupScheduleProjectConfig(projectName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {name = %q}`, projectName)
}

func snapshotScheduleCheck(t *testing.T, client *neon.Client, expectedFrequency string, expectedDay, expectedHour uint8, expectedRetention uint32) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		project, ok := state.RootModule().Resources["neon_project.this"]
		if !ok {
			return fmt.Errorf("resource neon_project.this not found in state")
		}
		branchID := project.Primary.Attributes["default_branch_id"]
		projectID := project.Primary.Attributes["id"]
		if projectID == "" || branchID == "" {
			return fmt.Errorf("project state is missing id or default_branch_id")
		}

		schedule, err := client.GetSnapshotSchedule(projectID, branchID)
		if err != nil {
			return err
		}
		assert.Len(t, schedule.Schedule, 1)
		assert.Equal(t, expectedFrequency, schedule.Schedule[0].Frequency)
		assert.Equal(t, expectedDay, *schedule.Schedule[0].Day)
		assert.Equal(t, expectedHour, *schedule.Schedule[0].Hour)
		assert.Equal(t, expectedRetention, *schedule.Schedule[0].RetentionSeconds)
		return nil
	}
}

func emptySnapshotScheduleCheck(t *testing.T, client *neon.Client) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		project, ok := state.RootModule().Resources["neon_project.this"]
		if !ok {
			return fmt.Errorf("resource neon_project.this not found in state")
		}
		projectID := project.Primary.Attributes["id"]
		branchID := project.Primary.Attributes["default_branch_id"]
		if projectID == "" || branchID == "" {
			return fmt.Errorf("project state is missing id or default_branch_id")
		}

		schedule, err := client.GetSnapshotSchedule(projectID, branchID)
		if err != nil {
			return err
		}
		assert.Len(t, schedule.Schedule, 0)
		return nil
	}
}

func TestBranchBackupSchedule(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "branchBackupSchedule"

	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	projectName := newProjectName(projectNamePrefix)
	resource.Test(
		t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newBackupScheduleConfig(projectName, "weekly", 1, 4, 604800),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.#", "1",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.frequency", "weekly",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.day", "1",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.hour", "4",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.retention_seconds", "604800",
						),
						snapshotScheduleCheck(t, client, "weekly", 1, 4, 604800),
					),
				},
				// update: mutate existing monthly schedule
				{
					Config: newBackupScheduleConfig(projectName, "monthly", 2, 5, 2592000),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.#", "1",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.frequency", "monthly",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.day", "2",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.hour", "5",
						),
						resource.TestCheckResourceAttr(
							"neon_branch_backup_schedule.this",
							"schedule.0.retention_seconds", "2592000",
						),
						snapshotScheduleCheck(t, client, "monthly", 2, 5, 2592000),
					),
				},
				// delete
				{
					Config: newBackupScheduleProjectConfig(projectName),
					Check: resource.ComposeTestCheckFunc(
						emptySnapshotScheduleCheck(t, client),
					),
				},
			},
		})
}
