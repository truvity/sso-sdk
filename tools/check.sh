#!/usr/bin/env bash
# Build/verify every SDK — the single entrypoint CI and humans share.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Go"
(cd sso && go build ./... && go vet ./...)

echo "==> TypeScript"
(cd typescript && npm ci --no-audit --no-fund && npm run build)

echo "==> Python"
(cd python && python3 -m compileall -q src)

echo "==> Java"
(cd java && mvn -q -B compile)

echo "ALL SDKS OK"
