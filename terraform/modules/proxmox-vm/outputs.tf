output "guests" {
  description = "Per-guest metadata consumed by the root outputs"
  value = {
    for name, cfg in var.vm_configurations : name => {
      kind  = "vm"
      vm_id = cfg.vm_id
      ip    = local.vm_ip[name]
      fqdn  = "${name}.${var.domain_suffix}"
    }
  }
}
