# Daedalus SDN zone + VNet, internal to Crete. Pattern matches
# worklab/terraform/foundation/proxmox/main.tf.

resource "proxmox_sdn_zone_vlan" "daedalus" {
  id     = var.zone_id
  bridge = var.bridge
  nodes  = [var.proxmox_node]
}

resource "proxmox_sdn_vnet" "daedalus" {
  id    = var.vnet_id
  zone  = proxmox_sdn_zone_vlan.daedalus.id
  tag   = var.vlan_id
  alias = "Daedalus VNet (VLAN ${var.vlan_id})"
}

resource "proxmox_sdn_subnet" "daedalus" {
  vnet    = proxmox_sdn_vnet.daedalus.id
  cidr    = var.subnet
  gateway = cidrhost(var.subnet, 1)
  # DHCP is served by the OPNsense LAN interface (static .1 gateway), not
  # by SDN dnsmasq — the OPNsense provider configures that later.
}

# Apply SDN changes; without this the zone/vnet are defined in pending
# state and bridges never appear on the node.
resource "proxmox_sdn_applier" "daedalus" {
  depends_on = [
    proxmox_sdn_zone_vlan.daedalus,
    proxmox_sdn_vnet.daedalus,
    proxmox_sdn_subnet.daedalus,
  ]

  lifecycle {
    replace_triggered_by = [
      proxmox_sdn_zone_vlan.daedalus,
      proxmox_sdn_vnet.daedalus,
      proxmox_sdn_subnet.daedalus,
    ]
  }
}
