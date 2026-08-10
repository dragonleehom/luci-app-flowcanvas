#!/usr/bin/env sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PACKAGE_DIR="$ROOT_DIR/luci-app-flowcanvas"

require_file() {
	if [ ! -f "$1" ]; then
		echo "error: required package file missing: $1" >&2
		exit 1
	fi
}

require_executable() {
	require_file "$1"
	if [ ! -x "$1" ]; then
		echo "error: expected executable package file: $1" >&2
		exit 1
	fi
}

require_file "$PACKAGE_DIR/Makefile"
require_file "$PACKAGE_DIR/root/usr/share/luci/menu.d/luci-app-flowcanvas.json"
require_file "$PACKAGE_DIR/root/usr/share/rpcd/acl.d/luci-app-flowcanvas.json"
require_file "$PACKAGE_DIR/ucode/controller/flowcanvas.uc"
require_file "$PACKAGE_DIR/htdocs/luci-static/resources/view/flowcanvas/console.js"
require_file "$PACKAGE_DIR/htdocs/luci-static/resources/flowcanvas/index.html"
require_file "$PACKAGE_DIR/src/backend/vendor/modules.txt"
require_executable "$PACKAGE_DIR/root/etc/init.d/flowcanvas"
require_executable "$PACKAGE_DIR/root/usr/libexec/flowcanvas/check-environment"

jq empty "$PACKAGE_DIR/root/usr/share/luci/menu.d/luci-app-flowcanvas.json"
jq empty "$PACKAGE_DIR/root/usr/share/rpcd/acl.d/luci-app-flowcanvas.json"
sh -n "$PACKAGE_DIR/root/etc/init.d/flowcanvas"
sh -n "$PACKAGE_DIR/root/usr/libexec/flowcanvas/check-environment"

grep -q 'ucode-mod-socket' "$PACKAGE_DIR/Makefile"
grep -q '+mihomo' "$PACKAGE_DIR/Makefile"
grep -q 'procd_set_param env' "$PACKAGE_DIR/root/etc/init.d/flowcanvas"
grep -q 'FLOWCANVAS_LISTEN=127.0.0.1:' "$PACKAGE_DIR/root/etc/init.d/flowcanvas"
grep -q 'FLOWCANVAS_MIHOMO_CONFIG=' "$PACKAGE_DIR/root/etc/init.d/flowcanvas"
grep -q "is_read_path_allowed" "$PACKAGE_DIR/ucode/controller/flowcanvas.uc"
if grep -q "canvas/events" "$PACKAGE_DIR/ucode/controller/flowcanvas.uc"; then
	echo "error: LuCI CGI proxy must not expose backend SSE endpoint" >&2
	exit 1
fi
grep -q "luci.controller.flowcanvas" "$PACKAGE_DIR/root/usr/share/luci/menu.d/luci-app-flowcanvas.json"

echo "OpenWrt package layout checks passed."
