include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-flowcanvas
PKG_VERSION:=0.4.0
PKG_RELEASE:=8
PKG_LICENSE:=MIT
PKG_MAINTAINER:=dragonleehom
PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

# The CI preparation step supplies a vendored backend and compiled React assets.
# Keeping Node.js outside the SDK makes the package build deterministic and small.
define Build/Prepare
	$(if $(wildcard $(CURDIR)/src/backend/vendor/modules.txt),,$(error Missing staged backend/vendor. Run scripts/prepare-openwrt-package.sh first))
	$(if $(wildcard $(CURDIR)/htdocs/luci-static/resources/flowcanvas/index.html),,$(error Missing LuCI frontend assets. Run scripts/prepare-openwrt-package.sh first))
	$(RM) -rf $(PKG_BUILD_DIR)
	$(INSTALL_DIR) $(PKG_BUILD_DIR)
	$(CP) $(CURDIR)/src/backend/. $(PKG_BUILD_DIR)/
endef

define Build/Compile
	(cd $(PKG_BUILD_DIR); \
		$(GO_BIN_PATH) \
		GOOS="$(GO_OS)" GOARCH="$(GO_ARCH)" GOAMD64="$(GO_AMD64)" \
		CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local GOPROXY=off \
		go build -mod=vendor -buildvcs=false -trimpath \
			-ldflags='-s -w -X main.version=$(PKG_VERSION)-$(PKG_RELEASE)' \
			-o $(PKG_BUILD_DIR)/flowcanvasd ./cmd/flowcanvasd)
endef

define Package/$(PKG_NAME)
	SECTION:=luci
	CATEGORY:=LuCI
	SUBMENU:=3. Applications
	TITLE:=FlowCanvas Intent-Based Networking Console
	URL:=https://github.com/dragonleehom/luci-app-flowcanvas
	DEPENDS:=@TARGET_x86_64 +luci-base +ucode +ucode-mod-socket +rpcd +sqlite3-cli +ca-bundle +mihomo
	PKGARCH:=x86_64
endef

define Package/$(PKG_NAME)/description
	LuCI interface and high-performance local control plane for dynamic
	Mihomo intent-based traffic orchestration with SQLite history, visual
	React Flow topology, audited rules compilation, reload and rollback.
endef

define Package/$(PKG_NAME)/conffiles
/etc/config/flowcanvas
endef

define Package/$(PKG_NAME)/install
	$(INSTALL_DIR) $(1)/usr/libexec $(1)/usr/share/ucode/luci/controller $(1)/www
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/flowcanvasd $(1)/usr/libexec/flowcanvasd
	$(INSTALL_DATA) $(CURDIR)/ucode/controller/flowcanvas.uc $(1)/usr/share/ucode/luci/controller/flowcanvas.uc
	$(CP) $(CURDIR)/htdocs/* $(1)/www/
	$(CP) $(CURDIR)/root/* $(1)/
endef

define Package/$(PKG_NAME)/preinst
#!/bin/sh
[ -n "$$IPKG_INSTROOT" ] && exit 0
set -u
missing=""
need_command() {
	command -v "$$1" >/dev/null 2>&1 || missing="$$missing $$2"
}
need_file() {
	[ -e "$$1" ] || missing="$$missing $$2"
}
need_command mihomo mihomo
need_command sqlite3 sqlite3-cli
need_command ucode ucode
need_file /usr/lib/ucode/socket.so ucode-mod-socket
if [ -n "$$missing" ]; then
	echo "flowcanvas: required runtime dependencies are unavailable:$$missing" >&2
	echo "flowcanvas: package dependency resolution must complete before installation; run 'opkg update && opkg install$$missing' and retry" >&2
	exit 1
fi
exit 0
endef

define Package/$(PKG_NAME)/postinst
#!/bin/sh
IPKG_INSTROOT="$${IPKG_INSTROOT:-}"
[ -n "$$IPKG_INSTROOT" ] && exit 0
[ -f /etc/config/flowcanvas ] && chmod 0600 /etc/config/flowcanvas
mkdir -p /var/lib/flowcanvas
chmod 0750 /var/lib/flowcanvas
rm -f /tmp/luci-indexcache.*
rm -rf /tmp/luci-modulecache/
/etc/init.d/rpcd reload 2>/dev/null || true
. /lib/functions.sh
config_load flowcanvas
config_get_bool enabled main enabled 0
if [ "$$enabled" -eq 1 ]; then
	/usr/libexec/flowcanvas/check-environment || exit 1
	/etc/init.d/flowcanvas enable
	/etc/init.d/flowcanvas restart || exit 1
else
	echo "flowcanvas: installed but disabled; configure /etc/config/flowcanvas then enable the service" >&2
fi
exit 0
endef

$(eval $(call BuildPackage,$(PKG_NAME)))
