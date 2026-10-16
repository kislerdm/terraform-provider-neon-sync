data "neon_project" "example" {
  id = "your-project-id"
}

output "database_host" {
  value = data.neon_project.example.database_host
}
