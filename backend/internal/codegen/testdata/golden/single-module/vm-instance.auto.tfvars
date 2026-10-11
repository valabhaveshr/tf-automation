vm_instance_instances = {
  web-01 = {
    admin_password = "S3cr3t!"
    image          = "ubuntu-22.04"
    name           = "prj-acme1-prod-web-01"
    size           = "Standard_B2s"
    tags = {
      env = "prod"
    }
  }
  web-02 = {
    admin_password = "An0ther$ecret"
    image          = "ubuntu-22.04"
    name           = "prj-acme1-prod-web-02"
    size           = "Standard_B4ms"
    tags           = {}
  }
}
