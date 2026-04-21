# Consolidated outputs for downstream Ansible / SSH automation.
# Every guest surfaces both management and services IPs; consumers pick
# whichever VLAN their workflow targets.

output "guests" {
  description = "Map of guest name to its IPs and metadata"
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
              ansible_host  = g.management_ip
              ansible_user  = var.admin_username
              services_ip   = g.services_ip
              guest_type    = g.kind
              proxmox_vm_id = g.vm_id
            }
          }
        }
      }
    }
  })
}
