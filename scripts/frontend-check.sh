#!/usr/bin/env bash
# Usage: scripts/frontend-check.sh <command>
# Runs command with sh in frontend/: here, or with SYNOPKGLAND_DOCKER_SSH=<ssh
# host> on Linux in Docker on that host, so this machine's load cannot slow
# its tests. There it runs in the official Node.js image, in a gate slot
# (scripts/docker-engine.sh), in the working tree with its untracked files
# (scripts/docker-tree.sh), after a frozen pnpm install (pnpm from corepack);
# the pnpm store is in the host's shared synopkgland-cache-npm volume.
# SYNOPKGLAND_DOCKER_NPM_REGISTRY: the registry pnpm uses there, for a network
# that blocks registry.npmjs.org; the lockfile's integrity still holds. The host's CA bundle is trusted there, for a network
# that inspects TLS. vitest takes half the host's CPUs (VITEST_MAX_WORKERS):
# the host's other gate slot has the rest. The container is
# <SYNOPKGLAND_CONTAINER_PREFIX (default syno-pkg-gopeed)>-frontend.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
command=$1
if test -z "${SYNOPKGLAND_DOCKER_SSH:-}"; then
	cd "$root/frontend"
	exec sh -c "$command"
fi

. "$root/scripts/docker-engine.sh"
registry=${SYNOPKGLAND_DOCKER_NPM_REGISTRY:-}
case $registry in *[!A-Za-z0-9:/._-]*) echo "invalid SYNOPKGLAND_DOCKER_NPM_REGISTRY: $registry" >&2; exit 2 ;; esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$root"
git ls-files -z --others --exclude-standard | COPYFILE_DISABLE=1 tar --no-xattrs -c --null -T - -f "$work/untracked.tar"
# The CA bundle is mounted only when the host has one.
ca=/etc/ssl/certs/ca-certificates.crt
image=public.ecr.aws/docker/library/node:24.20.0-bookworm@sha256:be23f54a88d34e8824c741b19b91064094f92c1c97b194144bfc8b50d67258e2
cache_volumes "$image" synopkgland-cache-npm
SYNOPKGLAND_DOCKER_SLOT=1 SYNOPKGLAND_DOCKER_TAR="$work/untracked.tar" \
SYNOPKGLAND_DOCKER_ARGS="-e CI=1 -e VITEST_MAX_WORKERS=\$((\$(nproc) / 2)) -v synopkgland-cache-npm:/cache/npm -e npm_config_cache=/cache/npm -e COREPACK_HOME=/cache/npm/corepack -e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 \
${registry:+-e npm_config_registry=$registry -e COREPACK_NPM_REGISTRY=$registry} \$(test -f $ca && echo -v $ca:/tmp/host-ca.crt:ro -e NODE_EXTRA_CA_CERTS=/tmp/host-ca.crt)" \
"$root/scripts/docker-tree.sh" "${SYNOPKGLAND_CONTAINER_PREFIX:-syno-pkg-gopeed}-frontend" "$image" \
	"mkdir -p /tmp/bin && export PATH=/tmp/bin:\$PATH && cd frontend && corepack enable --install-directory /tmp/bin pnpm && pnpm install --frozen-lockfile --store-dir=/cache/npm/pnpm-store --reporter=silent${registry:+ --registry=$registry} && $command"
