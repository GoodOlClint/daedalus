#!/bin/bash
# Build the claude-code worker + argus-sidecar images locally, save them
# as tars, scp to the labyrinth VM, and import into k3s's containerd.
# Avoids needing a remote registry during Phase 1.
#
# Run from the operator's workstation inside the repo root:
#   LABYRINTH_HOST=172.16.140.102 deploy/images-push.sh
#
# Optional env:
#   IMAGE_TAG=local              # tag applied to both images
#   REGISTRY_PREFIX=daedalus     # image name prefix — keep unless you push elsewhere
#   SSH_USER=daedalus            # ssh user on labyrinth

set -euo pipefail

: "${LABYRINTH_HOST:?must be set (e.g. 172.16.140.102)}"
: "${IMAGE_TAG:=local}"
: "${REGISTRY_PREFIX:=daedalus}"
: "${SSH_USER:=daedalus}"

WORKER_IMAGE="${REGISTRY_PREFIX}/claude-code:${IMAGE_TAG}"
SIDECAR_IMAGE="${REGISTRY_PREFIX}/argus-sidecar:${IMAGE_TAG}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Building ${WORKER_IMAGE}"
docker build -t "${WORKER_IMAGE}" ./agents/claude-code

echo "==> Building ${SIDECAR_IMAGE}"
docker build -f agents/sidecar/argus/Dockerfile -t "${SIDECAR_IMAGE}" .

echo "==> Saving images to tar"
docker save "${WORKER_IMAGE}" -o "${TMPDIR}/claude-code.tar"
docker save "${SIDECAR_IMAGE}" -o "${TMPDIR}/argus-sidecar.tar"

echo "==> scp to labyrinth:/tmp"
scp "${TMPDIR}/claude-code.tar" "${TMPDIR}/argus-sidecar.tar" "${SSH_USER}@${LABYRINTH_HOST}:/tmp/"

echo "==> k3s ctr images import"
ssh "${SSH_USER}@${LABYRINTH_HOST}" 'sudo bash -s' <<'SSH_EOF'
set -euo pipefail
sudo k3s ctr images import /tmp/claude-code.tar
sudo k3s ctr images import /tmp/argus-sidecar.tar
rm -f /tmp/claude-code.tar /tmp/argus-sidecar.tar
sudo k3s ctr images ls | grep daedalus/ || true
SSH_EOF

echo "==> Done. Use these image names in minos config:"
echo "    plugin_image:          ${WORKER_IMAGE}"
echo "    argus_sidecar_image:   ${SIDECAR_IMAGE}"
