variable "instance_count" {
  type        = number
  default     = 1
  description = "Number of instances."
}

variable "name" {
  type        = string
  default     = "example"
  description = "Resource name."
}

variable "enabled" {
  type        = bool
  default     = true
  description = "Whether the resource is enabled."
}
