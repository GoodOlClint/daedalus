output "guests" {
  description = "Per-LXC metadata consumed by the root outputs"
  value = {
    for name, cfg in var.lxc_configurations : name => {
      kind  = "lxc"
      vm_id = cfg.vm_id
      fqdn  = "${name}.${var.domain_suffix}"
      mac   = try(proxmox_virtual_environment_container.zakros[name].network_interface[0].mac_address, "")
      # bpg/proxmox surfaces the container's runtime IPv4 as a per-NIC
      # map (computed). Pick the first non-empty entry; pairs with
      # wait_for_ip{ipv4=true} on the resource so the value is populated
      # by the time downstream outputs read it.
      ip = try([
        for v in values(proxmox_virtual_environment_container.zakros[name].ipv4) : v
        if v != "" && v != "127.0.0.1" && !startswith(v, "fe80")
      ][0], "")
    }
  }
}
