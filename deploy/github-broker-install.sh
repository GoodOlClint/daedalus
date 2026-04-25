#!/bin/bash
# Install the github-broker daemon on the Minos VM. Co-located with
# Minos: same secrets.json, separate config, separate systemd unit,
# its own listen port (8082 by default).
#
# Prerequisites on the operator side:
#   * Go toolchain
#   * deploy/secrets.json populated with:
#       minos/signing-key-pub  — PEM public key from `minosctl gen-signing-key`
#       github/app-private-key — PEM private key from your GitHub App
#   * deploy/github-broker.json populated with App ID + installation ID
#
# Env:
#   MINOS_HOST   default: terraform output -> guests.minos.ip
#   SSH_USER     default zakros

set -euo pipefail

. "$(dirname "$0")/lib.sh"
: "${MINOS_HOST:=$(tf_guest_ip minos 2>/dev/null || true)}"
: "${MINOS_HOST:?run terraform apply so the minos guest is in state, or set MINOS_HOST manually}"
: "${SSH_USER:=zakros}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

for f in deploy/secrets.json deploy/github-broker.json; do
  if [ ! -f "$f" ]; then
    echo "Missing: $f" >&2
    if [ "$f" = "deploy/github-broker.json" ]; then
      echo "Copy from deploy/templates/github-broker.json.example and fill in your App ID + installation ID." >&2
    fi
    exit 1
  fi
done

echo "==> Building github-broker binary for linux/amd64"
mkdir -p bin
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/github-broker-linux-amd64 ./cmd/github-broker

echo "==> Staging files to scp"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp bin/github-broker-linux-amd64                 "$STAGE/github-broker"
cp deploy/github-broker.json                     "$STAGE/github-broker.json"
cp deploy/templates/github-broker.service        "$STAGE/github-broker.service"

ssh "${SSH_USER}@${MINOS_HOST}" 'mkdir -p /tmp/zakros-deploy'
scp "$STAGE"/* "${SSH_USER}@${MINOS_HOST}:/tmp/zakros-deploy/"

echo "==> Installing on ${MINOS_HOST}"
ssh "${SSH_USER}@${MINOS_HOST}" 'sudo bash -s' <<'SSH_EOF'
set -euo pipefail
STAGE=/tmp/zakros-deploy

id zakros >/dev/null

install -o root -g root -m 0755 "$STAGE/github-broker" /usr/local/bin/github-broker

install -d -o root -g root     -m 0755 /etc/zakros
install -o root    -g zakros   -m 0640 "$STAGE/github-broker.json" /etc/zakros/github-broker.json
# Symlink the secrets file Minos already manages — broker reads
# minos/signing-key-pub and github/app-private-key from the same file.
if [ ! -e /etc/zakros/secrets.json ]; then
  ln -s /etc/minos/secrets.json /etc/zakros/secrets.json
fi

install -o root -g root -m 0644 "$STAGE/github-broker.service" /etc/systemd/system/github-broker.service

systemctl daemon-reload
systemctl enable github-broker
systemctl restart github-broker

rm -rf "$STAGE"

echo "---"
systemctl --no-pager --full status github-broker | head -15 || true
SSH_EOF

echo "==> Done. Tail logs with:"
echo "    ssh ${SSH_USER}@${MINOS_HOST} 'sudo journalctl -u github-broker -f'"
