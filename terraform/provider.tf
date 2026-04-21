# Terraform provider configuration.
# Daedalus depends on Proxmox VLANs/bridges already existing on Crete — either
# configured via the sibling homelab/ repo's modules/network or by hand. This
# Terraform project only provisions the four Daedalus guests.

terraform {
  required_version = ">= 1.9"
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "~> 0.78"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
  }
}

provider "proxmox" {
  endpoint  = var.proxmox_endpoint
  api_token = var.proxmox_api_token
  insecure  = var.proxmox_insecure

  ssh {
    agent    = true
    username = var.proxmox_ssh_user
  }
}
