# Provider access. Credentials are loaded from TF_VAR_* environment
# variables — never from committed tfvars files.

variable "proxmox_endpoint" {
  type        = string
  description = "Proxmox VE API endpoint, e.g. https://crete.lan:8006/"
}

variable "proxmox_api_token" {
  type        = string
  description = "API token in '<user>!<token-id>=<uuid>' form"
  sensitive   = true
}

variable "proxmox_insecure" {
  type        = bool
  description = "Skip TLS verification of the Proxmox endpoint"
  default     = true
}

variable "proxmox_ssh_user" {
  type        = string
  description = "SSH user on the Proxmox host (bpg/proxmox uses SSH for some actions)"
  default     = "root"
}

# Node / storage targets.

variable "proxmox_node" {
  type        = string
  description = "Proxmox node name to place all Daedalus guests on"
  default     = "crete"
}

variable "primary_datastore" {
  type        = string
  description = "Primary storage pool for VM and LXC disks"
  default     = "local-zfs"
}

variable "image_datastore" {
  type        = string
  description = "Storage pool for cloud images and snippets"
  default     = "local"
}

# VLAN topology. Daedalus does NOT manage SDN — the VLANs below must already
# exist on the Proxmox node, either via homelab/ Terraform or manual config.
# Example (fill into vars.auto.tfvars):
#   vlans = {
#     mgmt = {
#       vlan_id = 10
#       bridge  = "vmbr0"
#       subnet  = "10.10.0.0/24"
#     }
#     services = {
#       vlan_id = 40
#       bridge  = "vmbr0"
#       subnet  = "10.40.0.0/24"
#     }
#   }

variable "vlans" {
  description = "VLANs the Daedalus guests attach to"
  type = map(object({
    vlan_id = number
    bridge  = string
    subnet  = string
  }))
}

variable "management_vlan" {
  type        = string
  description = "Key in `vlans` that carries SSH/default route"
  default     = "mgmt"
}

variable "services_vlan" {
  type        = string
  description = "Key in `vlans` for intra-Daedalus service traffic"
  default     = "services"
}

variable "dns_servers" {
  type        = list(string)
  description = "DNS servers for cloud-init network config"
  default     = ["1.1.1.1", "9.9.9.9"]
}

# Cloud-init defaults.

variable "admin_username" {
  type        = string
  description = "Admin user created inside every guest via cloud-init"
  default     = "daedalus"
}

variable "admin_password_hash" {
  type        = string
  description = "Pre-hashed admin password (mkpasswd -m sha-512). Empty disables password auth — SSH key only."
  default     = ""
  sensitive   = true
}

variable "ssh_public_key_path" {
  type        = string
  description = "Path to the operator SSH public key to inject into every guest"
  default     = "~/.ssh/id_ed25519.pub"
}

variable "timezone" {
  type        = string
  description = "Guest timezone"
  default     = "America/Chicago"
}

variable "domain_suffix" {
  type        = string
  description = "Domain suffix for guest FQDNs"
  default     = "daedalus.local"
}

# Image selection — operator usually leaves defaults. create_cloud_image
# downloads the Ubuntu Noble cloud image the first time; subsequent applies
# reuse it.

variable "create_cloud_image" {
  type        = bool
  description = "Download the Ubuntu Noble cloud image into Proxmox on apply"
  default     = true
}

variable "ubuntu_cloud_image_url" {
  type        = string
  description = "Ubuntu cloud image source URL"
  default     = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
}

variable "debian_lxc_template" {
  type        = string
  description = "LXC template name (as Proxmox sees it) for the Postgres container"
  default     = "debian-12-standard_12.7-1_amd64.tar.zst"
}

variable "debian_lxc_template_datastore" {
  type        = string
  description = "Storage pool that holds the LXC template tarball"
  default     = "local"
}
