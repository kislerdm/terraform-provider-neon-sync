package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestEndpoint(t *testing.T) {
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

	var verify = func(state *terraform.State) error {
		r, ok := state.RootModule().Resources["neon_endpoint.this"]
		assert.Truef(t, ok, "no endpoint resource found")
		resp, err := client.GetProjectEndpoint(r.Primary.Attributes["project_id"], r.Primary.ID)
		if err != nil {
			return err
		}
		assert.Equal(t, resp.Endpoint.RegionID, r.Primary.Attributes["region_id"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.PasswordlessAccess), r.Primary.Attributes["passwordless_access"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.Disabled), r.Primary.Attributes["disabled"])
		assert.Equal(t, string(resp.Endpoint.Provisioner), r.Primary.Attributes["compute_provisioner"])
		assert.Equal(t, resp.Endpoint.Host, r.Primary.Attributes["host"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.AutoscalingLimitMinCu),
			r.Primary.Attributes["autoscaling_limit_min_cu"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.AutoscalingLimitMaxCu),
			r.Primary.Attributes["autoscaling_limit_max_cu"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.SuspendTimeoutSeconds),
			r.Primary.Attributes["suspend_timeout_seconds"])
		assert.Equal(t, resp.Endpoint.Type.String(), r.Primary.Attributes["type"])
		if resp.Endpoint.Name != nil {
			assert.Equal(t, *resp.Endpoint.Name, r.Primary.Attributes["name"])
		}
		if resp.Endpoint.Settings.PgSettings != nil && len(*resp.Endpoint.Settings.PgSettings) > 0 {
			var actual neon.PgSettingsData
			actualRaw, ok := r.Primary.Attributes["pg_settings"]
			assert.Truef(t, ok, "no pg_settings attribute found")
			assert.NoErrorf(t, json.Unmarshal([]byte(actualRaw), &actual), "could not deserialize pg_settings")
			assert.EqualValuesf(t, *resp.Endpoint.Settings.PgSettings, actual,
				"unexpected pg_settings")
		}
		if resp.Endpoint.Settings.PreloadLibraries != nil {
			cnt, ok := r.Primary.Attributes["enabled_libraries.#"]
			assert.Truef(t, ok, "unexpected size of enabled_libraries")
			assert.EqualValuesf(t,
				fmt.Sprintf("%d", len(resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries)), cnt,
				"unexpected size of enabled_libraries")

			if len(resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries) > 0 {
				for i, lib := range resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries {
					assert.Equalf(t, lib, r.Primary.Attributes[fmt.Sprintf("enabled_libraries.%d", i)],
						"unexpected value for enabled_libraries[%d]", i)
				}
			}

			actualRaw, ok := r.Primary.Attributes["enabled_libraries_use_defaults"]
			if resp.Endpoint.Settings.PreloadLibraries.UseDefaults == nil {
				assert.Truef(t, !ok || actualRaw == "", "unexpected enabled_libraries_use_defaults value")
			} else {
				assert.Truef(t, ok, "no enabled_libraries_use_defaults attribute found")
				actual, err := strconv.ParseBool(actualRaw)
				assert.NoErrorf(t, err, "could not parse enabled_libraries_use_defaults")
				assert.EqualValuesf(t, *resp.Endpoint.Settings.PreloadLibraries.UseDefaults, actual,
					"unexpected enabled_libraries_use_defaults")
			}

		}
		return nil
	}

	t.Run("shall create read-only endpoint for default branch with inherited autoscaling config "+
		"and update its attr in-place", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
					resource "neon_endpoint" "this" {
						project_id = neon_project.this.id
						branch_id  = neon_project.this.default_branch_id
						region_id  = neon_project.this.region_id
						type       = "read_only"
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "type", "read_only"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host_pooler"),
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_endpoint.this"]
								assert.Truef(t, ok, "no endpoint resource found")

								host, ok := r.Primary.Attributes["host"]
								assert.Truef(t, ok, `"host" attribute not found`)

								hostPooler, ok := r.Primary.Attributes["host_pooler"]
								assert.Truef(t, ok, `"host_pooler" attribute not found`)

								assert.Equal(t, host, strings.ReplaceAll(hostPooler, "-pooler", ""))
								return nil
							},
							verify,
						),
					},
					// rename
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.default_branch_id
											region_id  = neon_project.this.region_id
											type       = "read_only"
											name       = %q
										}
										`, projectName, "foo"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "name", "foo"),
							verify,
						),
					},
					// update autoscaling
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.default_branch_id
											region_id  = neon_project.this.region_id
											type       = "read_only"
					
											autoscaling_limit_min_cu = 0.25
											autoscaling_limit_max_cu = 0.5
											suspend_timeout_seconds  = 120
										}
										`, projectName),
						Check: resource.ComposeTestCheckFunc(
							// the name remains as set in the prev. step despite the config removal from the manifest
							resource.TestCheckResourceAttr("neon_endpoint.this", "name", "foo"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
							verify,
						),
					},
					// 	update passwordless access
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.default_branch_id
											region_id  = neon_project.this.region_id
											type       = "read_only"
											disabled   = true
										}
										`, projectName),
						Check: resource.ComposeTestCheckFunc(
							// the autoscaling configs remain as set in the prev. step despite the config removal from the manifest
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "disabled", "true"),
							verify,
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.default_branch_id
											region_id  = neon_project.this.region_id
											type       = "read_only"
										}
										`, projectName),
						Check: resource.ComposeTestCheckFunc(
							// the disabled config remains as set in the prev. step despite the config removal from the manifest
							resource.TestCheckResourceAttr("neon_endpoint.this", "disabled", "true"),
							verify,
						),
					},
					// update enabled_libraries
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
					resource "neon_endpoint" "this" {
						project_id        = neon_project.this.id
						branch_id         = neon_project.this.default_branch_id
						region_id         = neon_project.this.region_id
						type              = "read_only"
						enabled_libraries = ["uuid-ossp"]
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#",
								"1"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0",
								"uuid-ossp"),
							resource.TestCheckResourceAttr("neon_endpoint.this",
								"enabled_libraries_use_defaults", "false"),
							verify,
						),
					},
					// update enabled_libraries_use_defaults
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}

					resource "neon_endpoint" "this" {
						project_id                     = neon_project.this.id
						branch_id                      = neon_project.this.default_branch_id
						region_id                      = neon_project.this.region_id
						type                           = "read_only"
						enabled_libraries              = ["uuid-ossp"]
						enabled_libraries_use_defaults = true   
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#",
								"1"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0",
								"uuid-ossp"),
							resource.TestCheckResourceAttr("neon_endpoint.this",
								"enabled_libraries_use_defaults", "true"),
							verify,
						),
					},
					// rm enabled_libraries and enabled_libraries_use_defaults from the manifest
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}

					resource "neon_endpoint" "this" {
						project_id                     = neon_project.this.id
						branch_id                      = neon_project.this.default_branch_id
						region_id                      = neon_project.this.region_id
						type                           = "read_only"
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#",
								"1"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0",
								"uuid-ossp"),
							resource.TestCheckResourceAttr("neon_endpoint.this",
								"enabled_libraries_use_defaults", "true"),
							verify,
						),
					},
				},
			},
		)
	})

	t.Run("shall create a read-write endpoint", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					resource "neon_branch" "this" {project_id = neon_project.this.id}
					resource "neon_endpoint" "this" {
						project_id = neon_project.this.id
						branch_id  = neon_branch.this.id
						region_id  = neon_project.this.region_id
						type       = "read_write"
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "type", "read_write"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host_pooler"),
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_endpoint.this"]
								assert.Truef(t, ok, "no endpoint resource found")

								host, ok := r.Primary.Attributes["host"]
								assert.Truef(t, ok, `"host" attribute not found`)

								hostPooler, ok := r.Primary.Attributes["host_pooler"]
								assert.Truef(t, ok, `"host_pooler" attribute not found`)

								assert.Equal(t, host, strings.ReplaceAll(hostPooler, "-pooler", ""))
								return nil
							},
							verify,
						),
					},
				},
			},
		)
	})
}
