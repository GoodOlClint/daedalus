terraform {
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
