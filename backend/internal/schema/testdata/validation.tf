variable "size" {
  type        = string
  default     = "Standard_B2s"
  description = "VM SKU size."

  validation {
    condition     = contains(["Standard_B2s", "Standard_B4ms"], var.size)
    error_message = "size must be one of the approved SKUs."
  }
}
