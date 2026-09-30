variable "admin_password" {
  type        = string
  description = "Local admin password."
  sensitive   = true
  nullable    = false
}
