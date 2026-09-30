terraform {
  backend "s3" {
    bucket = "org-tfstate-bucket"
    prefix = "acme/webshop/prod"
  }
}
