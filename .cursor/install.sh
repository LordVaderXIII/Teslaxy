#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

npm ci --prefix frontend --no-audit --no-fund
(cd backend && go mod download)
