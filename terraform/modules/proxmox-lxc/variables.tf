variable "proxmox_node" { type = string }
variable "primary_datastore" { type = string }
variable "template_file" {
  type        = string
  description = "Template tarball filename (as seen by Proxmox storage)"
}
variable "template_datastore" {
  type        = string
  description = "Storage pool holding the LXC template tarball"
}

variable "vlans" {
  type = map(object({
    vlan_id = number
    bridge  = string
    subnet  = string
  }))
}
variable "management_vlan" { type = string }
variable "services_vlan" { type = string }
variable "dns_servers" { type = list(string) }

variable "admin_username" { type = string }
variable "ssh_public_key_path" { type = string }
variable "timezone" { type = string }
variable "domain_suffix" { type = string }

variable "lxc_configurations" {
  description = "Map of LXC name -> config"
  type = map(object({
    vm_id              = number
    description        = string
    cpu_cores          = number
    memory_mb          = number
    disk_size_gb       = number
    vlans              = list(string)
    services_ip_offset = number
  }))
}
