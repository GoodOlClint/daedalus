data "local_file" "ssh_public_key" {
  filename = pathexpand(var.ssh_public_key_path)
}

# Download the Ubuntu cloud image once; reused on subsequent applies.
resource "proxmox_virtual_environment_download_file" "cloud_image" {
  count = var.create_cloud_image ? 1 : 0

  content_type        = "iso"
  datastore_id        = var.image_datastore
  node_name           = var.proxmox_node
  url                 = var.cloud_image_url
  file_name           = "noble-server-cloudimg-amd64.img"
  overwrite           = true
  overwrite_unmanaged = true
  verify              = true
  upload_timeout      = 600
}

locals {
  cloud_image_file_id = var.create_cloud_image ? proxmox_virtual_environment_download_file.cloud_image[0].id : "${var.image_datastore}:iso/noble-server-cloudimg-amd64.img"

  # Per-VM management IP (mgmt VLAN, host = vm_id).
  management_ips = {
    for name, cfg in var.vm_configurations :
    name => cidrhost(var.vlans[var.management_vlan].subnet, cfg.vm_id)
  }

  # Per-VM services IP (services VLAN, host = services_ip_offset).
  services_ips = {
    for name, cfg in var.vm_configurations :
    name => cidrhost(var.vlans[var.services_vlan].subnet, cfg.services_ip_offset)
  }

  # Stable MAC byte derived from vm_id so regenerations do not flap DHCP.
  mac_byte = {
    for name, cfg in var.vm_configurations :
    name => (cfg.vm_id % 254) + 1
  }

  # Build per-VLAN interface descriptor for each VM.
  vm_interfaces = {
    for name, cfg in var.vm_configurations : name => [
      for vlan_key in cfg.vlans : {
        vlan_key = vlan_key
        vlan_id  = var.vlans[vlan_key].vlan_id
        bridge   = var.vlans[vlan_key].bridge
        subnet   = var.vlans[vlan_key].subnet
        ip = vlan_key == var.management_vlan ? local.management_ips[name] : (
          vlan_key == var.services_vlan ? local.services_ips[name] : null
        )
        prefix     = tonumber(split("/", var.vlans[vlan_key].subnet)[1])
        gateway    = cidrhost(var.vlans[vlan_key].subnet, 1)
        is_primary = vlan_key == var.management_vlan
        mac = format(
          "52:54:00:%02x:%02x:%02x",
          local.mac_byte[name],
          floor(var.vlans[vlan_key].vlan_id / 256),
          var.vlans[vlan_key].vlan_id % 256
        )
      }
    ]
  }
}

resource "proxmox_virtual_environment_file" "user_data" {
  for_each = var.vm_configurations

  content_type = "snippets"
  datastore_id = var.image_datastore
  node_name    = var.proxmox_node

  source_raw {
    data = templatefile("${path.module}/templates/user-data.yaml.tmpl", {
      hostname      = each.value.vm_id == null ? each.key : each.key
      fqdn          = "${each.key}.${var.domain_suffix}"
      username      = var.admin_username
      ssh_key       = trimspace(data.local_file.ssh_public_key.content)
      timezone      = var.timezone
      password_hash = var.admin_password_hash
    })
    file_name = "${each.key}-user-data.yaml"
  }
}

resource "proxmox_virtual_environment_file" "network_data" {
  for_each = var.vm_configurations

  content_type = "snippets"
  datastore_id = var.image_datastore
  node_name    = var.proxmox_node

  source_raw {
    data = templatefile("${path.module}/templates/network-data.yaml.tmpl", {
      interfaces  = local.vm_interfaces[each.key]
      dns_servers = var.dns_servers
    })
    file_name = "${each.key}-network-data.yaml"
  }
}

resource "proxmox_virtual_environment_vm" "daedalus" {
  for_each = var.vm_configurations

  name        = each.key
  vm_id       = each.value.vm_id
  node_name   = var.proxmox_node
  description = each.value.description
  machine     = "q35"
  tags        = ["daedalus", each.key]

  agent {
    enabled = true
  }

  cpu {
    cores = each.value.cpu_cores
    type  = "x86-64-v3"
  }

  memory {
    dedicated = each.value.memory_mb
  }

  disk {
    datastore_id = var.primary_datastore
    file_id      = local.cloud_image_file_id
    interface    = "virtio0"
    iothread     = true
    discard      = "on"
    size         = each.value.disk_size_gb
  }

  initialization {
    datastore_id         = var.primary_datastore
    user_data_file_id    = proxmox_virtual_environment_file.user_data[each.key].id
    network_data_file_id = proxmox_virtual_environment_file.network_data[each.key].id
  }

  dynamic "network_device" {
    for_each = local.vm_interfaces[each.key]
    content {
      bridge      = network_device.value.bridge
      vlan_id     = can(regex("^vmbr", network_device.value.bridge)) ? network_device.value.vlan_id : null
      mac_address = network_device.value.mac
    }
  }

  # Cloud-init files are rendered once at first boot; ignore subsequent
  # edits so template changes do not force VM recreation.
  lifecycle {
    ignore_changes = [
      initialization[0].user_data_file_id,
      initialization[0].network_data_file_id,
    ]
  }
}
