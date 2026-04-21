data "local_file" "ssh_public_key" {
  filename = pathexpand(var.ssh_public_key_path)
}

locals {
  management_ips = {
    for name, cfg in var.lxc_configurations :
    name => cidrhost(var.vlans[var.management_vlan].subnet, cfg.vm_id)
  }
  services_ips = {
    for name, cfg in var.lxc_configurations :
    name => cidrhost(var.vlans[var.services_vlan].subnet, cfg.services_ip_offset)
  }
  prefix_for_vlan = {
    for k, v in var.vlans :
    k => tonumber(split("/", v.subnet)[1])
  }
  gateway_for_vlan = {
    for k, v in var.vlans :
    k => cidrhost(v.subnet, 1)
  }

  # Per-LXC ordered interface list — same ordering drives both
  # network_interface blocks and initialization.ip_config blocks below.
  lxc_interfaces = {
    for name, cfg in var.lxc_configurations : name => [
      for vlan_key in cfg.vlans : {
        vlan_key = vlan_key
        vlan_id  = var.vlans[vlan_key].vlan_id
        bridge   = var.vlans[vlan_key].bridge
        ip_cidr = format(
          "%s/%d",
          vlan_key == var.management_vlan ? local.management_ips[name] : local.services_ips[name],
          local.prefix_for_vlan[vlan_key],
        )
        gateway    = local.gateway_for_vlan[vlan_key]
        is_primary = vlan_key == var.management_vlan
      }
    ]
  }
}

resource "proxmox_virtual_environment_container" "daedalus" {
  for_each = var.lxc_configurations

  node_name     = var.proxmox_node
  vm_id         = each.value.vm_id
  description   = each.value.description
  tags          = ["daedalus", each.key, "lxc"]
  unprivileged  = true
  start_on_boot = true

  cpu {
    cores = each.value.cpu_cores
  }

  memory {
    dedicated = each.value.memory_mb
  }

  disk {
    datastore_id = var.primary_datastore
    size         = each.value.disk_size_gb
  }

  operating_system {
    template_file_id = "${var.template_datastore}:vztmpl/${var.template_file}"
    type             = "debian"
  }

  initialization {
    hostname = each.key

    dns {
      servers = var.dns_servers
      domain  = var.domain_suffix
    }

    user_account {
      keys = [trimspace(data.local_file.ssh_public_key.content)]
    }

    # IP configs: one block per interface, in the same order as the
    # network_interface blocks below. Only the primary (management VLAN)
    # carries gateway4 so secondary interfaces do not fight over the
    # default route.
    dynamic "ip_config" {
      for_each = local.lxc_interfaces[each.key]
      content {
        ipv4 {
          address = ip_config.value.ip_cidr
          gateway = ip_config.value.is_primary ? ip_config.value.gateway : null
        }
      }
    }
  }

  dynamic "network_interface" {
    for_each = local.lxc_interfaces[each.key]
    content {
      name    = "eth${network_interface.key}"
      bridge  = network_interface.value.bridge
      vlan_id = can(regex("^vmbr", network_interface.value.bridge)) ? network_interface.value.vlan_id : null
    }
  }

  startup {
    order      = 1
    up_delay   = 10
    down_delay = 10
  }
}
