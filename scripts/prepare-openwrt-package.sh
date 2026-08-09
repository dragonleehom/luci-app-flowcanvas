#!/usr/bin/env sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_DIR="$ROOT_DIR/backend"
PACKAGE_DIR="$ROOT_DIR/luci-app-flowcanvas"
LUCISTATIC_DIR="$PACKAGE_DIR/htdocs/luci-static/resources/flowcanvas"
BACKEND_STAGE_DIR="$PACKAGE_DIR/src/backend"

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' was not found" >&2
		exit 1
	fi
}

require_command go
require_command pnpm

rm -rf "$LUCISTATIC_DIR" "$BACKEND_STAGE_DIR"
mkdir -p "$LUCISTATIC_DIR" "$BACKEND_STAGE_DIR"

(
	cd "$FRONTEND_DIR"
	pnpm install --frozen-lockfile
	pnpm build
)

(
	cd "$BACKEND_DIR"
	go mod vendor
)

# The OpenWrt feeds installer may expose the package directory through a
# symlink. Stage the complete Go module under the package itself so CURDIR is
# self-contained in both local SDK builds and GitHub Action containers.
cp -a "$BACKEND_DIR/." "$BACKEND_STAGE_DIR/"

if [ ! -f "$LUCISTATIC_DIR/index.html" ]; then
	echo "error: LuCI frontend build did not produce index.html" >&2
	exit 1
fi

if [ ! -f "$BACKEND_STAGE_DIR/vendor/modules.txt" ]; then
	echo "error: staged Go module does not contain vendor/modules.txt" >&2
	exit 1
fi

echo "OpenWrt package staging is ready."
