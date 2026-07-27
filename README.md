# luci-app-flowcanvas

> 面向 Mihomo 旁路由的**可视化意图驱动网络编排面板**。通过真实连接元数据形成“终端 → 动态应用流 → 出口”的可编辑画布，而非依赖静态 geosite/geoip 规则库。

**当前状态：第一阶段完成。** 本仓库已经具备可构建的 Go 控制面、SQLite 初始迁移、React Flow 画布、严格的三段式连线校验以及 v1 REST/SSE 契约。Mihomo WebSocket 写入、图到 YAML 编译、无损热重载、LuCI 包装与 GitHub Actions `.ipk` 发布将在后续阶段继续实现。

| 能力 | 第一阶段状态 | 说明 |
|---|---|---|
| Go 控制面 | 已实现骨架 | 同机 REST/SSE 服务、SQLite 初始化、健康检查与画布持久化。 |
| SQLite 历史记忆 | 已实现 schema 与事务层 | 含终端、应用、终端-应用组合、连接样本、画布边和编译审计表。 |
| Mihomo 连接模型 | 已实现客户端、归一化和 Watcher 契约 | 支持 `host` / `sniffHost`、源/目的 IP、TCP/UDP/QUIC 类型归一化。 |
| ARP / DHCP 终端发现 | 已实现解析器骨架 | 支持 `/proc/net/arp` 与 dnsmasq lease 解析。 |
| React Flow 画布 | 已实现 | 包含 Source、Filter、Target 三种节点、缩放网格和连线规则。 |
| Graph → Mihomo YAML | 预留 | 第三阶段实现。 |
| Mihomo 热重载 | 预留 | 第三阶段实现。 |
| LuCI `.ipk` 包与 CI 发布 | 预留 | 第四阶段实现。 |

## 设计原则

FlowCanvas **不会自行抓包、解密流量或读取请求正文**。它订阅 Mihomo 的 `WS /connections` 连接快照，并消费 Mihomo 已归一化的连接元数据。Mihomo 的连接 API 提供活动连接、连接元数据、流量统计、命中规则和代理链信息；`/proxies` 提供当前出口及其运行状态。[1] 这样既避免重复部署 L7 检测链路，也保证前端显示的是内核实际已经识别到的终端与 Host/SNI。

Mihomo 内置嗅探器目前支持 HTTP、TLS 和 QUIC；为减少纯 IP 流量漏识别，推荐启用 `sniffer.enable` 与 `parse-pure-ip`。[2] FlowCanvas 只在 `sniffHost` 或 `host` 存在可用域名时生成 Filter 节点，绝不根据目的 IP 反向臆测域名。

## 架构

```mermaid
flowchart LR
    subgraph LAN[局域网数据面]
        D[终端设备]
        M[Mihomo Core\nTUN / Redir / 透明代理]
        D --> M
    end

    subgraph FC[FlowCanvas 控制面（仅 loopback）]
        W[Connection Watcher\nWS /connections]
        F[动态特征归一化\nSNI / Host / Network]
        I[分片实时索引]
        S[(SQLite WAL\n历史记忆 + 画布)]
        A[ARP / DHCP\n拓扑解析]
        P[Proxy Catalog\nGET /proxies]
        API[REST + SSE API]
        G[图校验服务]
        C[规则编译器\n后续阶段]
    end

    subgraph UI[OpenWrt 管理面]
        L[LuCI 同源适配层]
        R[React + React Flow]
    end

    M -- "连接快照" --> W --> F --> I
    F --> S
    A --> S
    M -- "出口目录" --> P --> I
    I --> API
    S --> API
    API --> L --> R
    R --> L --> G --> S
    G -. "阶段三" .-> C -. "PUT /configs?force=true" .-> M
```

> Mihomo 路由规则按由上至下的顺序匹配；逻辑组合规则使用 `LOGIC_TYPE,((payload1),(payload2)),Proxy` 形式。[3] 因此后续编译器会将用户确认的图生成到独立管理规则段，而不会直接破坏已有的手工规则。

### 实时处理与内存策略

目标是高性能 x86 旁路由，因此设计优先级是低延迟和高并发，而非极限缩小内存。`ConnectionWatcher` 对完整 WebSocket 快照按 `connection.id` 做差分；新出现的连接生成 `observed` 事件，消失的连接生成 `closed` 事件。状态写入使用有界队列、批量 SQLite WAL 事务和独立 SSE 推送队列，防止慢浏览器拖慢嗅探与持久化热路径。

| 模块 | 初始策略 | 目的 |
|---|---|---|
| WebSocket 快照 | 默认目标 `250ms` 间隔，可配置 | 及时显示连接状态变化。 |
| 实时索引 | 计划 64 分片 map | 降低高并发连接更新时的锁竞争。 |
| 数据库写入 | 计划单写协程、256 条或 200ms 批量提交 | 提升 SQLite 写吞吐并缩短锁持有时间。 |
| SSE 客户端 | 每订阅者 256 条有界队列 | 慢客户端收到 `resync`，不阻塞核心数据流。 |
| ARP/DHCP 刷新 | 计划 30 秒周期 | 避免把拓扑扫描放进连接热路径。 |
| `/proxies` 刷新 | 计划 10 秒周期 | 获取出口可用性并支持手动刷新。 |

## 严格画布模型

画布中只能形成以下两类边：

```text
Source（终端） ──► Filter（该终端真实嗅探到的 Host/SNI） ──► Target（Mihomo 运行时出口）
```

| 节点 | 稳定标识 | 活跃状态 | 服务端校验 |
|---|---|---|---|
| Source | `source:{device_id}` | ARP/DHCP 在线或有活跃应用流 | 只能连到同一个 `device_id` 的 Filter。 |
| Filter | `filter:{device_application_id}` | 当前 `activeConnections > 0` | 只允许连接 Target。 |
| Target | `target:{hash(proxy_name)}` | Mihomo 目录存在，且显示 `alive` | 不能作为连线源。 |

前端会即时拒绝不合法拖线；后端在 `PUT /api/v1/canvas/graph` 中再次验证，因此无法通过伪造 HTTP 请求跳过约束。保存使用 `If-Match: "canvas-{revision}"` 乐观锁，多标签页并发修改会返回 `409 CANVAS_REVISION_CONFLICT`。

## 数据模型

SQLite 迁移位于 [`backend/migrations/0001_init.sql`](backend/migrations/0001_init.sql)，运行时以嵌入式 schema 初始化。主要实体关系如下：

```mermaid
erDiagram
    DEVICES ||--o{ DEVICE_APPLICATIONS : observes
    APPLICATIONS ||--o{ DEVICE_APPLICATIONS : classifies
    DEVICE_APPLICATIONS ||--o{ CONNECTION_SAMPLES : records
    CANVASES ||--o{ CANVAS_NODES : contains
    CANVASES ||--o{ CANVAS_EDGES : contains
    COMPILATION_REVISIONS }o--|| CANVASES : audits
```

`applications` 默认将首次观察到的域名设为精确 `DOMAIN` 匹配，避免系统自动把 `v.qq.com` 放大为 `qq.com`。用户在下一阶段明确确认匹配范围后，才可以提升为 `DOMAIN-SUFFIX` 或 `DOMAIN-KEYWORD`。Mihomo 的 `DOMAIN-SUFFIX` 按域名后缀匹配，`DOMAIN-KEYWORD` 按关键词匹配。[3]

完整的架构、字段优先级、SQL schema、错误码和 API 示例请参阅 [`docs/architecture.md`](docs/architecture.md)。

## 仓库结构

```text
.
├── backend/
│   ├── cmd/flowcanvasd/        # Go 守护进程入口
│   ├── internal/api/           # REST、SSE、API 错误封装
│   ├── internal/domain/        # 稳定领域模型与 ID
│   ├── internal/graph/         # 三段式图校验
│   ├── internal/mihomo/        # Controller 客户端、WS 模型、快照差分
│   ├── internal/store/         # SQLite WAL、迁移与图事务
│   ├── internal/telemetry/     # 实时画布目录接口
│   ├── internal/topology/      # ARP 和 DHCP lease 解析
│   └── migrations/             # 版本化 SQL 迁移
├── frontend/
│   └── src/                    # React 19 + React Flow 12 画布
├── luci-app-flowcanvas/        # 后续 OpenWrt 打包与 LuCI 资源落点
└── docs/architecture.md        # 第一阶段详细技术契约
```

## 本地开发与验证

### 前置条件

开发机需要 Go 1.22+、Node.js 22+ 和 pnpm。SQLite 通过纯 Go 驱动嵌入守护进程；生产打包阶段会依据 OpenWrt SDK 目标重新评估二进制尺寸、SQLite 依赖和交叉编译策略。

### 启动后端

```bash
cd backend
GOPROXY=https://proxy.golang.org,direct go mod tidy
go test ./...
mkdir -p ../bin
go build -o ../bin/flowcanvasd ./cmd/flowcanvasd

FLOWCANVAS_LISTEN=127.0.0.1:16789 \
FLOWCANVAS_DB=/tmp/flowcanvas/flowcanvas.db \
FLOWCANVAS_DEMO=true \
../bin/flowcanvasd
```

开发模式默认启用演示目录，便于在没有 Mihomo 的机器上验证 API 和画布。生产环境将通过 UCI 配置加载 Mihomo Controller 地址、Secret、数据库路径和运行模式；该适配将在打包阶段落地。

### 启动前端

```bash
cd frontend
pnpm install
pnpm build
pnpm dev
```

本地 Vite 开发时如果无法连接 `/api/v1/canvas`，界面会回退到不可持久化的演示快照；生产 LuCI 环境不会使用这个回退逻辑。

### 已验证命令

```bash
cd backend && go test ./...
cd frontend && pnpm build
```

第一阶段还验证了：守护进程能初始化 SQLite 数据库；`GET /api/v1/health` 返回数据库状态；`GET /api/v1/canvas` 返回三类节点；合法的 `Source → Filter → Target` 图能够原子保存并将 ETag 从 `canvas-0` 升级到 `canvas-1`；非法 `Source → Target` 图返回 `422 INVALID_EDGE_DIRECTION`。

## v1 API 概览

| 方法 | 路径 | 第一阶段状态 |
|---|---|---|
| `GET` | `/api/v1/health` | 可用。 |
| `GET` | `/api/v1/canvas` | 可用。 |
| `GET` | `/api/v1/canvas/events` | 可用 SSE 骨架。 |
| `PUT` | `/api/v1/canvas/graph` | 可用，服务端严格校验。 |
| `GET` | `/api/v1/targets` | 可用演示目录，第二阶段改为 Mihomo 实时目录。 |
| `GET` | `/api/v1/features` | 可用演示目录，第二阶段改为 SQLite 历史记忆。 |
| `POST` | `/api/v1/discovery/refresh` | 已预留。 |
| `POST` | `/api/v1/compilations/validate` | 已预留，第三阶段实现。 |
| `POST` | `/api/v1/compilations/apply` | 已预留，第三阶段实现。 |

## 推荐的 Mihomo 前置配置

在第二阶段接入前，请确保 Mihomo 控制器只监听 loopback，并启用内置域名嗅探。`override-destination` 是否打开取决于现有 Fake-IP/TUN 配置，本项目不会在第一阶段自动修改它。

```yaml
external-controller: 127.0.0.1:9090
secret: "<仅由 root 可读的随机字符串>"
sniffer:
  enable: true
  parse-pure-ip: true
  sniff:
    HTTP: { ports: [80, 8080-8880] }
    TLS: { ports: [443, 8443] }
    QUIC: { ports: [443, 8443] }
```

Mihomo 的 `external-controller` 可通过 RESTful API 管理内核，`external-ui` 可承载静态网页资源；生产安装将使用 LuCI 同源适配层而非把 Controller Secret 发送至浏览器。[4]

## 安全与隐私

FlowCanvas 处理的是源 IP、MAC、终端名称、域名特征和出口状态等本地网络元数据。它不上传这些数据，不记录 HTTP 请求体、Cookie、TLS 私钥或明文负载。Mihomo Secret 只应由 root 权限的后端读取；前端应通过 LuCI 已认证会话调用本地 API，而不能直接调用 `127.0.0.1:9090`。

## 路线图

| 阶段 | 重点 | 目标交付 |
|---|---|---|
| 第二阶段 | Mihomo WebSocket、SQLite 状态写入、ARP/DHCP 与 `/proxies` 实时目录 | 活跃/历史节点的真实数据流。 |
| 第三阶段 | 图→YAML 规则编译和 `/configs?force=true` 热重载 | 可审计的 `SRC-IP-CIDR + DOMAIN*` 复合规则及回滚。 |
| 第四阶段 | LuCI 菜单与同源代理、OpenWrt SDK Makefile、安装脚本、GitHub Actions | x86_64 `.ipk` 自动构建与 Release 发布。 |

## 许可证

本项目采用 [MIT License](LICENSE)。

## 参考资料

[1]: https://wiki.metacubex.one/en/api/ "Mihomo 官方 API 文档"
[2]: https://wiki.metacubex.one/en/config/sniff/ "Mihomo 官方域名嗅探配置"
[3]: https://wiki.metacubex.one/en/config/rules/ "Mihomo 官方路由规则文档"
[4]: https://wiki.metacubex.one/en/config/general/ "Mihomo 官方通用配置文档"
