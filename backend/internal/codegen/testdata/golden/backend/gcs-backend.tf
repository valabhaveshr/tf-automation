terraform {
  backend "gcs" {
    bucket = "org-tfstate-bucket"
    prefix = "acme/webshop/prod"
  }
}
