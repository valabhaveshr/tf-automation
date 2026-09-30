variable "weird" {
  type        = frobnicate
  description = "Uses an exotic type expression typeexpr cannot constraint-parse; must fall back to type=unsupported rather than failing the whole file."
}
