# Daedalus deployment scripts

Bootstrap scripts for the four Daedalus guests after Terraform has
provisioned them on Crete. All scripts are idempotent — safe to re-run
after fixing config or adjusting env.

Assumes the flat-VLAN topology (VLAN 140, 172.16.140.0/24, DHCP). Current
IPs: postgres .100, minos .101, labyrinth .102, ariadne .103.

## 1. Postgres LXC (vmid 211)

```sh
export POSTGRES_PASSWORD="$(openssl rand -base64 32 | tr -d '=+/' | head -c 32)"
echo "$POSTGRES_PASSWORD"   # stash — needed for migrations + minos config

ssh root@172.16.30.103 \
  "POSTGRES_PASSWORD='$POSTGRES_PASSWORD' pct exec 211 -- bash" \
  < deploy/postgres-bootstrap.sh
```

Then run migrations from your workstation:

```sh
go install github.com/pressly/goose/v3/cmd/goose@latest
DSN="postgres://daedalus:$POSTGRES_PASSWORD@172.16.140.100:5432/daedalus?sslmode=disable"
~/go/bin/goose -dir minos/storage/pgstore/migrations postgres "$DSN" up
```

## 2. k3s on labyrinth (vmid 212)

```sh
ssh daedalus@172.16.140.102 'sudo bash -s' < deploy/k3s-install.sh

# pull kubeconfig back
scp daedalus@172.16.140.102:/etc/rancher/k3s/k3s.yaml ~/.kube/daedalus.yaml
sed -i '' 's/127.0.0.1/172.16.140.102/' ~/.kube/daedalus.yaml  # drop '' on Linux
KUBECONFIG=~/.kube/daedalus.yaml kubectl get nodes
```

## 3. Worker images → labyrinth's containerd

```sh
LABYRINTH_HOST=172.16.140.102 deploy/images-push.sh
```

Builds `daedalus/claude-code:local` + `daedalus/argus-sidecar:local` locally,
scps tars, imports into k3s's containerd. No remote registry needed.

## 4. Minos on minos VM (vmid 210)

First, copy the config + secrets templates and fill in real values:

```sh
cp deploy/templates/config.json.example  deploy/config.json
cp deploy/templates/secrets.json.example deploy/secrets.json
# edit both — both are gitignored
```

Things to replace in `config.json`:
- `REPLACE_POSTGRES_PASSWORD` → the password you generated in step 1
- `REPLACE_YOUR_DISCORD_USER_ID` → your Discord user ID (enable Developer Mode, right-click yourself, Copy User ID)
- `REPLACE_DISCORD_CHANNEL_ID` → the Discord channel where Minos creates task threads

Things to replace in `secrets.json`:
- `minos/bearer` and `minos/admin-token` — `openssl rand -base64 32` each
- `cerberus/github-webhook` — any strong random string; configure the same value in the GitHub App webhook secret field
- `hermes/discord-bot-token` — your Discord bot token

Then:

```sh
deploy/minos-install.sh

# tail logs
ssh daedalus@172.16.140.101 'sudo journalctl -u minos -f'
```

The script builds `bin/minos`, scps it + config + secrets + kubeconfig,
writes the systemd unit, starts the service. Idempotent — re-run to push
config changes.

## 5. GitHub webhook ingress

Still pending. Options:
- FRP tunnel from minos VM out to a public endpoint
- Cloudflare Tunnel (no port-forward, authenticated ingress)
- ngrok (dev only)

Target: `POST <public-hostname>/cerberus/webhook` reaches the minos daemon
on 172.16.140.101:8080.

## 6. End-to-end smoke test

1. `/status` in Discord → minos should respond with operational summary
2. `/commission "echo hello"` → pod spawns on labyrinth, runs entrypoint,
   opens PR on the test repo, audit row lands in postgres
3. `minosctl replay <run-id>` from operator workstation → prints the full
   task trace
