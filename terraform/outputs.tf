# Consolidated outputs. VM IPs are harvested from qemu-guest-agent
# (populated on refresh once the agent is up); LXC IPs come from the
# bpg/proxmox provider's computed `ipv4` map, paired with
# wait_for_ip{ipv4=true} on the resource so they're populated by the
# time the apply returns. MACs are deterministic for VMs (derived from
# vm_id + ip_offset) and Proxmox-assigned for LXCs.

output "guests" {
  description = "Map of guest name to its metadata"
  value       = merge(module.vms.guests, module.lxcs.guests)
}

output "guests_yaml" {
  description = "YAML-encoded guest inventory, suitable for piping into other tooling"
  value = yamlencode({
    for name, g in merge(module.vms.guests, module.lxcs.guests) :
    name => {
      hostname = g.fqdn
      ip       = g.ip
      type     = g.kind
      vmid     = g.vm_id
      mac      = g.mac
    }
  })
}
