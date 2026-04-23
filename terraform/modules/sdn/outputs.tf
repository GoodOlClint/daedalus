output "bridge" {
  description = "SDN VNet id — pass this as the bridge argument to VM/LXC modules"
  value       = proxmox_virtual_environment_sdn_vnet.daedalus.id
}

output "applier_id" {
  description = "Reference callers depend on so VM/LXC creates wait for SDN apply"
  value       = proxmox_virtual_environment_sdn_applier.daedalus.id
}
