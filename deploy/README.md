# Daedalus deployment scripts

Bootstrap scripts for the four Daedalus guests after Terraform has
provisioned them on Crete. All scripts are idempotent — safe to re-run
after fixing config or adjusting env.

## 1. Postgres LXC (vmid 211)

The Postgres container hosts the shared database for Minos, Argus,
Mnemosyne, and Iris (4 schemas, all in one DB).

```sh
# generate a strong password once and stash it in your password manager
export POSTGRES_PASSWORD="$(openssl rand -base64 32 | tr -d '=+/' | head -c 32)"

# pipe the script into pct exec on Crete; env vars are sent via SSH
ssh root@<crete-ip> \
  "POSTGRES_PASSWORD='$POSTGRES_PASSWORD' pct exec 211 -- bash" \
  < deploy/postgres-bootstrap.sh
```

The script installs Postgres 17 from PGDG, enables pgvector, creates
the `daedalus` role + DB, and opens listen_addresses to `*` (defaults
`pg_hba` to `0.0.0.0/0` since the LXC is on internal VLAN 140).

After it completes, run the goose migrations from your workstation:

```sh
DAEDALUS_PG_HOST=<postgres-vm-ip>
DSN="postgres://daedalus:$POSTGRES_PASSWORD@$DAEDALUS_PG_HOST:5432/daedalus?sslmode=disable"
goose -dir minos/storage/pgstore/migrations postgres "$DSN" up
```

## 2. k3s on labyrinth (vmid 212)

Single-node k3s cluster that hosts worker pods (claude-code +
argus sidecar). Traefik disabled — Daedalus uses FRP/Cloudflare for
public ingress, not in-cluster ingress.

```sh
ssh daedalus@<labyrinth-ip> 'sudo bash -s' < deploy/k3s-install.sh
```

Pull the kubeconfig back per the script's final hint. Minos's dispatcher
reads it via `KUBECONFIG`.

## 3. Minos VM (vmid 210)

(Pending — minos systemd unit + binary deploy comes next.)
