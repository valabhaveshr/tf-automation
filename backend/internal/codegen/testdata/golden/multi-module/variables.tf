variable "vm_instance_instances" {
  type = map(object({ size = string, image = string, admin_password = string, name = string, tags = map(string) }))
}

variable "storage_bucket_instances" {
  type = map(object({ tier = string }))
}
