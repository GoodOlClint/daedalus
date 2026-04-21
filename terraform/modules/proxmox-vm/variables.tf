variable "proxmox_node" { type = string }
variable "primary_datastore" { type = string }
variable "image_datastore" { type = string }
variable "create_cloud_image" { type = bool }
variable "cloud_image_url" { type = string }

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
variable "admin_password_hash" { type = string }
variable "ssh_public_key_path" { type = string }
variable "timezone" { type = string }
variable "domain_suffix" { type = string }

variable "vm_configurations" {
  description = "Map of VM name -> config"
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
