module "vm_instance" {
  source         = "git::https://github.com/org/modules.git//vm-instance?ref=vm-instance/v1.2.0"
  for_each       = var.vm_instance_instances
  size           = each.value.size
  image          = each.value.image
  admin_password = each.value.admin_password
  name           = each.value.name
  tags           = each.value.tags
}
