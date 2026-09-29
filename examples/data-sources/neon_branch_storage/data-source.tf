data "neon_branch_storage" "this" {
  project_id = "cool-moon-42"
  branch_id  = "br-cool-moon-42"
}

output "s3_endpoint" {
  value = data.neon_branch_storage.this.s3_endpoint
}

output "region" {
  value = data.neon_branch_storage.this.region
}

output "force_path_style" {
  value = data.neon_branch_storage.this.force_path_style
}