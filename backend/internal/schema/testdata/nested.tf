variable "disks" {
  type = list(object({
    name    = string
    size_gb = number
    tags    = map(string)
  }))
  description = "Extra disks attached to the instance."
  default     = []
}
