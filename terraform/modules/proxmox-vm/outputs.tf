output "guests" {
  description = "Per-guest metadata consumed by the root outputs"
  value = {
    for name, cfg in var.vm_configurations : name => {
      kind          = "vm"
      vm_id         = cfg.vm_id
      management_ip = local.management_ips[name]
      services_ip   = local.services_ips[name]
      fqdn          = "${name}.${var.domain_suffix}"
    }
  }
}
