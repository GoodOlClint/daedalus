# Consolidated outputs for downstream Ansible / SSH automation.
# Every guest surfaces its single Daedalus VNet IP; the firewall
# output carries the LAN gateway address operators use to reach the
# OPNsense web UI (default https://10.100.0.1/).

output "firewall" {
  description = "OPNsense firewall metadata"
  value = {
    vm_id   = module.firewall.vm_id
    lan_ip  = module.firewall.lan_ip
    api_url = "https://${module.firewall.lan_ip}/"
  }
}

output "guests" {
  description = "Map of guest name to its IP and metadata"
  value       = merge(module.vms.guests, module.lxcs.guests)
}

output "ansible_inventory_yaml" {
  description = "Ansible-style inventory ready to write to inventory.yaml"
  value = yamlencode({
    all = {
      children = {
        daedalus = {
          hosts = {
            for name, g in merge(module.vms.guests, module.lxcs.guests) :
            name => {
              ansible_host  = g.ip
              ansible_user  = var.admin_username
              guest_type    = g.kind
              proxmox_vm_id = g.vm_id
            }
          }
        }
      }
    }
  })
}
