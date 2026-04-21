module "vms" {
  source = "./modules/proxmox-vm"

  proxmox_node       = var.proxmox_node
  primary_datastore  = var.primary_datastore
  image_datastore    = var.image_datastore
  create_cloud_image = var.create_cloud_image
  cloud_image_url    = var.ubuntu_cloud_image_url

  vlans           = var.vlans
  management_vlan = var.management_vlan
  services_vlan   = var.services_vlan
  dns_servers     = var.dns_servers

  admin_username      = var.admin_username
  admin_password_hash = var.admin_password_hash
  ssh_public_key_path = var.ssh_public_key_path
  timezone            = var.timezone
  domain_suffix       = var.domain_suffix

  vm_configurations = local.vm_guests
}

module "lxcs" {
  source = "./modules/proxmox-lxc"

  proxmox_node      = var.proxmox_node
  primary_datastore = var.primary_datastore

  template_file      = var.debian_lxc_template
  template_datastore = var.debian_lxc_template_datastore

  vlans           = var.vlans
  management_vlan = var.management_vlan
  services_vlan   = var.services_vlan
  dns_servers     = var.dns_servers

  admin_username      = var.admin_username
  ssh_public_key_path = var.ssh_public_key_path
  timezone            = var.timezone
  domain_suffix       = var.domain_suffix

  lxc_configurations = local.lxc_guests
}
