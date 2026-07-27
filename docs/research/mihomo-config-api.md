# Mihomo 配置重载 API 研究记录

**检索日期：** 2026-07-27  
**用途：** 为 FlowCanvas 第三阶段的规则编译、热重载、失败回滚和审计设计提供可追溯依据。

## 官方 API 契约

Mihomo 官方 API 文档说明，运行配置端点为 `GET /configs` 和 `PUT /configs?force=true`。后者用于重新加载基础配置，成功响应为 `HTTP 204 No Content`。用于配置路径或内联 YAML payload 的请求体形状为：

```json
{"path":"","payload":""}
```

请求应携带 `Authorization: Bearer ${secret}`。若使用 `path` 且目标路径不在 Mihomo 工作目录内，必须由 Mihomo 进程通过 `SAFE_PATHS` 显式允许该目录。[1]

| API | FlowCanvas 用途 | 成功判定 |
|---|---|---|
| `GET /configs` | 读取运行配置的有限运行时视图；不依赖其作为可逆完整 YAML 源。 | `200 OK`。 |
| `PUT /configs?force=true` | 使用持久化的完整主配置路径重新加载 Mihomo。 | `204 No Content`。 |
| `GET /proxies` | 在成功应用或回滚后刷新 Target 目录。 | `200 OK`。 |
| `GET /rules` | 可选的事后观测，不作为编译正确性的唯一验证。 | `200 OK`。 |

## 设计结论

FlowCanvas 不会把 `GET /configs` 的 JSON 响应当作完整 YAML 配置的序列化来源；因为控制器返回的是运行时基础配置视图，不承诺逐字保留原始 YAML、注释、锚点或未知扩展字段。安全方案是由 FlowCanvas 管理主 Mihomo YAML 中具有保留前缀的**inline classical rule-provider 条目**，每个 Target 一个 provider，再为它生成对应的 `RULE-SET,<provider>,<target>` 顶层规则。这样每个 provider 的 payload 只负责匹配，顶层 `RULE-SET` 负责出口动作：

```yaml
rule-providers:
  flowcanvas-2d8474b1:
    type: inline
    behavior: classical
    payload:
      - AND,((SRC-IP-CIDR,192.168.1.50/32),(DOMAIN,v.qq.com))
rules:
  - RULE-SET,flowcanvas-2d8474b1,Proxy-US
  - MATCH,DIRECT
```

之所以不把 `Proxy-US` 写入 classical payload，是因为 Mihomo 的 `RuleSet` 在匹配时采用外层 `RULE-SET` 的 adapter；classical strategy 只保留每项规则的布尔匹配结果，内部 rule adapter 不决定最终出口。[4] FlowCanvas 因此会保留用户主配置的其他节点，原子写入仅更新保留前缀的 managed entries。

写入流程必须使用临时文件、`fsync`、原子 rename 与内容哈希；成功写入后调用 `PUT /configs?force=true`。若重载返回非 204、网络失败或事后出口目录刷新失败，则恢复上一个已审计的完整主配置备份并再次调用重载 API。SQLite 审计表只记录受管 overlay YAML、哈希与备份路径，不保存可能含代理凭据的完整主 YAML。

> 规则的评估顺序决定行为。FlowCanvas 保持画布边的稳定排序，针对同一终端-应用流的多个 Target 关系拒绝歧义，而不是在生成 YAML 时隐式选择任意一个出口。

## 安全限制

* Mihomo Secret 只驻留在守护进程内，绝不写入 SQLite 审计表或返回给浏览器。
* 主配置路径及 FlowCanvas 片段路径由 UCI/环境变量显式配置；调用前验证其位于允许的本地目录。
* 不接受用户经 HTTP 提交任意 YAML。浏览器只能保存严格校验过的图；服务端从图生成文本。
* 新规则的编译预览是无副作用操作；只有显式的 apply 请求会写入规则文件和调用 Mihomo 控制器。
* 回滚必须是同步操作，只有回滚重载成功才将审计状态标记为 `rolled_back`；否则记录 `rollback_failed` 并向控制面返回高优先级错误。

## References

[1]: https://wiki.metacubex.one/en/api/ "Mihomo 官方 API 文档：Running Configuration"

## 规则与 rule-provider 格式补充

Mihomo 规则按从上到下的顺序匹配；`DOMAIN` 是完整域名匹配，`DOMAIN-SUFFIX` 是后缀匹配，`DOMAIN-KEYWORD` 是关键词匹配。逻辑规则的官方形式为 `LOGIC_TYPE,((payload1),(payload2)),Proxy`，其中 payload 可为 `SRC-IP-CIDR,192.168.1.50/32` 与 `DOMAIN,v.qq.com` 的组合。[2]

`rule-providers` 支持 `inline`、`file` 等类型与 `classical` behavior；FlowCanvas 在第三阶段采用 `inline`，因而无需额外触及 HomeDir/`SAFE_PATHS` 路径约束。[3] 受管 payload 只包含匹配器：

```yaml
payload:
  - AND,((SRC-IP-CIDR,192.168.1.50/32),(DOMAIN,v.qq.com))
```

编译器允许的匹配映射如下：

| FlowCanvas `match.kind` | Mihomo 规则前缀 |
|---|---|
| `domain` | `DOMAIN` |
| `suffix` | `DOMAIN-SUFFIX` |
| `keyword` | `DOMAIN-KEYWORD` |

任何 Filter 没有 Target、被多个 Target 指向、包含无效源 IP、空匹配值，或引用当前不存在的 Target 时都会被本地校验拒绝；不会生成“部分可用”规则。

[2]: https://wiki.metacubex.one/en/config/rules/ "Mihomo 官方路由规则文档"
[3]: https://wiki.metacubex.one/en/config/rule-providers/ "Mihomo 官方 rule-provider 文档"

## 上游源码核验

对 Mihomo `hub/route/configs.go` 的 `updateConfigs` 实现核验显示：控制器首先解码 `{path,payload}`，再在调用 `executor.ApplyConfig(cfg, force)` **之前**解析 payload 或配置路径。解析失败、非绝对路径或不安全路径均返回 `HTTP 400`；只有成功解析后才执行 `ApplyConfig`，并返回 `HTTP 204`。这支持 FlowCanvas 将 `204` 作为热重载成功判定，将任何非 204 或网络错误视为需要恢复上一个规则片段的失败状态。[5]

[4]: https://github.com/MetaCubeX/mihomo/tree/Meta/rules/provider "Mihomo 上游 RuleSet 与 classical provider 实现"
[5]: https://github.com/MetaCubeX/mihomo/blob/Meta/hub/route/configs.go "Mihomo 上游 updateConfigs 实现"
