#!/usr/bin/env bash
# Builds the Svelte UI and copies it to the directory embedded in the Go binary.
set -euo pipefail
cd "$(dirname "$0")"
(cd ui && npm ci && npm run build)
rm -rf internal/server/ui-dist
cp -r ui/dist internal/server/ui-dist
echo "UI copied to internal/server/ui-dist"
