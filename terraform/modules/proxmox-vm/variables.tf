variable "proxmox_node" { type = string }
variable "primary_datastore" { type = string }
variable "image_datastore" { type = string }
variable "create_cloud_image" { type = bool }
variable "cloud_image_url" { type = string }

# Network — one NIC per guest on the Daedalus SDN VNet. Bridge name is
# the SDN VNet id (Proxmox exposes VNets as bridges after sdn apply).
variable "bridge" {
  type        = string
  description = "Bridge (SDN VNet id) each guest attaches to"
}
variable "subnet" {
  type        = string
  description = "CIDR for the Daedalus network (gateway = first host)"
}
variable "dns_servers" { type = list(string) }
variable "domain_suffix" { type = string }

variable "admin_username" { type = string }
variable "admin_password_hash" { type = string }
variable "ssh_public_key_path" { type = string }
variable "timezone" { type = string }

variable "vm_configurations" {
  description = "Map of VM name -> config"
  type = map(object({
    vm_id        = number
    description  = string
    cpu_cores    = number
    memory_mb    = number
    disk_size_gb = number
    ip_offset    = number
  }))
}
