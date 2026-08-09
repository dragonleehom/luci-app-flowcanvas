# 第四阶段：LuCI、OpenWrt 打包与 CI 设计

## 目标与安全边界

第四阶段将 `flowcanvasd` 固定为仅监听 `127.0.0.1` 的本地守护进程。浏览器不再直接访问 `127.0.0.1:16789`，也绝不接触 Mihomo Controller Secret；所有生产浏览器请求都经 LuCI 已认证路由转发到同机 API。

> FlowCanvas 只将 `flowcanvas-` 命名空间内的受管规则写入 Mihomo 主配置。LuCI 同源桥接同样只允许明确列出的 `/api/v1` 资源，拒绝路径遍历、任意方法、任意上游地址和原生 SSE 长连接。

| 层级 | 实现 | 约束 |
|---|---|---|
| LuCI 菜单 | `admin/services/flowcanvas` 菜单 JSON + JS view | 依赖 `luci-app-flowcanvas` ACL。 |
| React 页面 | `/luci-static/flowcanvas/index.html` iframe | Vite 使用相对资源路径，避免覆盖 LuCI 全局 CSS。 |
| 同源桥接 | `luci.controller.flowcanvas` ucode controller | 只连接从 UCI `api_port` 读取的 `127.0.0.1:<port>`。 |
| 状态读接口 | `GET /health`、`/canvas`、`/targets`、`/features` | 无副作用但仍要求 LuCI cookie 登录和 ACL。 |
| 写接口 | 图保存、发现刷新、规则预览与应用 | 路由声明 `post: true`，LuCI 强制验证 `token` 参数。 |
| 实时更新 | LuCI iframe 内定时重拉 `/canvas` | CGI 不代理 `/canvas/events` SSE，避免一个浏览器连接长期占用 LuCI worker。 |
| 守护进程 | `/etc/init.d/flowcanvas` 的 procd 实例 | UCI 只配置端口数值，init 脚本强制使用 loopback。 |

## CSRF 与前端 API 基址

标准 LuCI dispatcher 对标注 `post: true` 的 function action 要求 `REQUEST_METHOD=POST` 且 `http.formvalue("token")` 等于会话 token。嵌入页面与 LuCI 同源，因此前端从 `window.parent.L.env.token` 读取 token，仅在 `POST` 与 `PUT` API URL 的 query 参数中附加 `token`。这是 LuCI 调度器需要的请求保护参数，而不是 Mihomo Secret。

前端在普通开发/裸机模式继续使用 `/api/v1` 及原生 SSE；检测到 `luci-static/flowcanvas` 路径时，自动改用：

```text
/cgi-bin/luci/admin/services/flowcanvas/api/v1
```

## UCI 与 procd 契约

`/etc/config/flowcanvas` 使用 `config flowcanvas 'main'`。默认 `enabled=0`，以避免安装时误改未知 Mihomo 配置。服务启动时将 UCI 字段导出为现有 Go 守护进程的 `FLOWCANVAS_*` 环境变量。

| UCI 字段 | Go 环境变量 | 默认值 |
|---|---|---|
| `api_port` | `FLOWCANVAS_LISTEN` | `127.0.0.1:16789`（init 强制 loopback） |
| `database` | `FLOWCANVAS_DB` | `/var/lib/flowcanvas/flowcanvas.db` |
| `mihomo_controller` | `FLOWCANVAS_MIHOMO_CONTROLLER` | `http://127.0.0.1:9090` |
| `mihomo_secret` | `FLOWCANVAS_MIHOMO_SECRET` | 空；仅 root 可读 UCI 文件中保存。 |
| `mihomo_config` | `FLOWCANVAS_MIHOMO_CONFIG` | `/etc/mihomo/config.yaml` |
| `mihomo_backup_dir` | `FLOWCANVAS_MIHOMO_BACKUP_DIR` | `/etc/mihomo/.flowcanvas-backups` |
| 队列、刷新与超时项 | 同名 `FLOWCANVAS_*` | 沿用 Go 默认性能参数。 |

## 打包与依赖

包名为 `luci-app-flowcanvas`，当前发布范围为 OpenWrt 24.10+ x86_64，因为认证同源代理依赖该系列提供的 `ucode-mod-socket`。运行时声明 `luci-base`、`ucode`、`ucode-mod-socket`、`rpcd`、`sqlite3-cli`、`ca-bundle` 与虚拟 `mihomo` 依赖。`mihomo-meta` 和 `mihomo-alpha` 在 Nikki feed 中均提供虚拟名 `mihomo`，所以系统可以保留已有的兼容内核。[1]

Go 守护进程采用 SDK 内的 Go host 工具链、`CGO_ENABLED=0`、`GOOS/GOARCH` 目标变量和仓库内 vendor 依赖构建。前端由 CI 先通过 `pnpm build` 构建，再由 `scripts/prepare-openwrt-package.sh` 生成并暂存 LuCI 静态资源与后端 vendor 依赖；Makefile 只安装已准备好的静态资源，从而不要求 SDK 镜像含 Node.js。

## 安装自检

`preinst` 在真实路由器（非 `IPKG_INSTROOT`）上检查 `mihomo`、`sqlite3`、`ucode` 和 `ucode-mod-socket`；发现缺失时先尝试 `opkg update && opkg install`，仍失败则以清晰错误退出。`postinst` 创建 root-only 数据/备份目录、收紧 `/etc/config/flowcanvas` 权限、刷新 LuCI/rpcd 缓存，并仅在 UCI 显式启用服务时执行启动前校验与重启。

## 参考资料

[1]: [OpenWrt/LuCI 打包研究](research/openwrt-luci-packaging.md)
