# OpenWrt 与 LuCI 打包研究结论

**调研日期：2026-07-28**

## SDK 与包构建

OpenWrt SDK 是面向特定目标平台的裁剪 Buildroot；官方 `openwrt/gh-action-sdk` 将仓库作为 `src-link` feed 挂载至容器 `/feed/`，执行 `feeds update/install` 后编译指定包，并将 `bin/` 与日志移至 artifact 目录。[1]

因此 FlowCanvas 的包目录必须直接位于仓库根的 `luci-app-flowcanvas/`，通过 `PACKAGES=luci-app-flowcanvas` 定向构建。GitHub Actions 使用 `ARCH=x86_64-24.10.8`，将实际 artifact 内的 `*.ipk` 与校验和上传至 Release。虽然 23.05 同样有 ucode 运行时，但其官方 core ucode Makefile 未提供 `ucode-mod-socket`；FlowCanvas 的认证同源代理依赖该模块，因此发布兼容范围明确为 **OpenWrt 24.10+ x86_64**，而不产生不可用的 23.05 包。

官方 Go 包框架通过 `golang-package.mk` 提供目标 `GOOS`/`GOARCH`、交叉 C 工具链与 Go 缓存环境。它会把 `PKG_BUILD_DIR` 复制至 GOPATH 中的 `GO_PKG` 路径，再执行 `go install`。[2] FlowCanvas 仓库的 Go module 位于 `backend/` 子目录，因此 package Makefile 将 `PKG_BUILD_DIR` 指向该子目录，并把 `GO_PKG` 固定为 `github.com/dragonleehom/luci-app-flowcanvas/backend`、`GO_PKG_BUILD_PKG` 固定为 `.../cmd/flowcanvasd`。

## LuCI 菜单、ACL 与同源代理

当前 LuCI 应用的标准布局是：JavaScript view 位于 `htdocs/luci-static/resources/view/`，菜单 JSON 位于 `root/usr/share/luci/menu.d/`，ACL JSON 位于 `root/usr/share/rpcd/acl.d/`；所有 `root/` 下的文件按路径自动安装。[3]

FlowCanvas 使用一个极薄的 LuCI JavaScript view 承载 iframe，iframe 加载 `/luci-static/resources/flowcanvas/index.html`。React SPA 保持相对静态资源路径，并将 API 基址配置为 LuCI 路由前缀 `/cgi-bin/luci/admin/services/flowcanvas/api/v1`。这使浏览器只与已认证的 LuCI 同源端点通信，Mihomo secret 始终只存在于守护进程的 root 进程环境中。

同源桥接采用认证的 LuCI **ucode controller**，而不是开放 Go 端口或第三方反向代理。上游 `luci-app-dockerman` 已使用 menu JSON `action.type=function`、cookie 登录验证和 ACL 依赖绑定 ucode controller 方法。[4] 它同时依赖 `ucode-mod-socket`，通过本地 socket 转发 HTTP 请求并把响应流式写回 CGI 输出。[5] FlowCanvas 复用这一模式，但严格限制可转发的方法、路径、头部和请求大小，并且只连接 `127.0.0.1:16789`。

由于 LuCI CGI 请求模型不适合长期 SSE 响应，代理会明确拒绝 `/canvas/events`。前端在嵌入 LuCI 时改用受控的低频画布轮询；裸机开发模式仍可以使用 Go 原生 SSE。

## Mihomo 依赖

官方 OpenWrt packages feed 不含 Mihomo。`nikkinikki-org/OpenWrt-nikki` feed 的 `mihomo-meta` 与 `mihomo-alpha` 包均 `PROVIDES:=mihomo`，因此 FlowCanvas 运行依赖可安全声明为虚拟 `+mihomo`，同时 GitHub Actions 添加该 feed 来满足 SDK 解析。[6]

这意味着在 iStoreOS 或其他下游系统中，只要已安装的内核包声明/提供 `mihomo`，依赖会正常满足；若下游包名不同，安装自检脚本仍会通过实际 `mihomo` 可执行文件和 Controller 健康检查给出清晰阻断信息。

## 参考资料

[1]: https://github.com/openwrt/gh-action-sdk "OpenWrt SDK GitHub Action"
[2]: https://github.com/openwrt/packages/blob/master/lang/golang/golang-package.mk "OpenWrt Go package framework"
[3]: https://github.com/openwrt/luci/tree/master/applications/luci-app-example "LuCI application example"
[4]: https://github.com/openwrt/luci/tree/master/applications/luci-app-dockerman "LuCI ucode controller routing example"
[5]: https://github.com/openwrt/luci/blob/master/applications/luci-app-dockerman/ucode/controller/docker.uc "LuCI local socket proxy example"
[6]: https://github.com/nikkinikki-org/OpenWrt-nikki/tree/main/mihomo-meta "Mihomo Meta OpenWrt package"
