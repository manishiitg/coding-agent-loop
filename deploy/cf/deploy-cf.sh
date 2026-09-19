#!/usr/bin/env bash
set -euo pipefail

# Backward-compatible Confida entry point. Confida uses the same parameterized
# rootless Linux deployment pipeline as SparkQuill; all target-specific values
# live in deploy/rootless-linux/products/confida/product.env.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/../rootless-linux/deploy.sh" confida "$@"
