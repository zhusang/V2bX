## Why

面板上的已用流量只有「用户 × 节点倍率 × 上传/下载」一个总数：V2bX 在 `SizeStatWriter` / `ConnCounter` / `LogTraffic` 里把每条连接的字节累加到用户总计数，上报 `UniProxy/push` 时只带 `uid → [up, down]`。目标网站在上报链路里完全不存在。

现有的 Xboard `AccessLogAnalysis` 插件靠节点上的 `log-parser.sh` 解析 Xray access log 补这个缺口，但 access log 只在连接**建立**时写一行，没有字节数，所以只能统计「请求次数」。管理员排查「这个用户 300G 流量花在哪」时，拿到的是次数排名，和流量排名可能完全不一致（一个视频连接几个 G，一千次 API 请求几 MB）。

要回答「谁在哪个网站用了多少流量」，必须在节点内核里、在已经计费的那条字节流上同时按目标归属，并由面板按天汇总。

## What Changes

**V2bX（节点）**

- **新增**按网站的流量采集器 `common/sitestat`：每条连接登记一个轻量计数对象，目标确定后绑定到站点；上报周期内按「用户 × 站点」采集增量，长连接跨周期、跨天也能按时段归属。
- **改造** Xray 分发器（`core/xray/app/dispatcher`）：在现有用户计数器上做「同时累加」，不新增外层 writer，保证 XTLS splice 零拷贝路径的用户计费不受影响；在 `routedDispatch` 嗅探和审计规则通过后绑定站点。
- **改造** sing-box 钩子（`core/sing/hook.go`）与 `common/counter/conn.go`：TCP/UDP 连接计数同时写入站点计数；**顺带修复** `ConnCounter.Read/Write` 误用 `Store(n)` 覆盖用户流量计数的计费 bug。
- **改造** Hysteria2 钩子（`core/hy2/hook.go`）：实现 `TraceStream/UntraceStream`，从 `StreamStats.Tx/Rx` 采集 TCP 流的站点流量；UDP 无目标信息，用「用户全量增量 − TCP 流增量」归入 `(udp)`。
- **新增**站点归并：域名按公共后缀归并为可注册域名（`rr3---sn-xx.googlevideo.com` → `googlevideo.com`），IP 原样保留，端口丢弃；可配置保留完整主机名。
- **新增**上报任务与面板 API 客户端 `ReportSiteTraffic`：独立周期（默认 300 秒）、批次 ID 幂等、失败排队重试、单请求条数上限；面板未安装插件（404）时本轮静默停用，不影响主流程。
- **新增**节点配置 `Options.SiteTraffic`（默认关闭），并同步补进 `NodeConfig.MarshalJSON`。
- `core.Core` 接口新增 `GetSiteTrafficSlice`，`Selector` 按节点转发。

**Xboard `AccessLogAnalysis` 插件（面板）**

- **新增**节点上报接口 `POST /api/v1/server/site-traffic/push`，复用核心 `server` 中间件（`server_token` + `node_id` + `node_type`），不再需要插件自己的 API Token，节点也无法冒充其他 `server_id`。
- **新增**表 `v2_site_traffic`（插件自带迁移），按 `(user_id, server_id, site, record_date)` 批量 upsert 累加上传、下载、连接数。
- **新增**后台查询：用户的网站流量明细（含占用户当日总流量比例）、网站流量排行、某网站的用户流量排行；页面新增「网站流量」视图。
- 保留期清理覆盖新表。
- 旧的 access log 次数统计保留，标记为**弃用**；`log-parser*.sh` 中硬编码的线上地址与 Token 移出仓库并要求轮换 Token。

## Capabilities

### New Capabilities

- `site-traffic-collection`：V2bX 节点在三种内核上按「用户 × 站点」采集上下行字节与连接数，并可靠地周期上报到面板。
- `site-traffic-panel`：Xboard 插件接收节点上报、按天汇总入库、提供后台查询与保留期清理。

### Modified Capabilities

<!-- V2bX 的 openspec/specs/ 下没有覆盖流量计数或上报的已归档 spec，本变更不做 delta。
     Xboard 侧现有 `access-log-analysis` spec 的行为不变（仅标记弃用），面板部分以新增 capability 表达；
     实施时把 `specs/site-traffic-panel/` 同步到 Xboard 仓库的 openspec 中。 -->

## Impact

- **V2bX 代码**
  - 新增 `common/sitestat/`（采集器、站点归并、测试）
  - `common/counter/xraytraffic.go`、`common/counter/conn.go`（同时累加、修复 `Store` bug）
  - `core/xray/app/dispatcher/default.go`、`countreader.go`
  - `core/xray/user.go`、`core/sing/hook.go`、`core/sing/user.go`、`core/hy2/hook.go`、`core/hy2/user.go`
  - `core/interface.go`、`core/selector.go`
  - `api/panel/`（新增 `site_traffic.go`）
  - `node/task.go`、`node/user.go`（新上报任务）
  - `conf/node.go`（新配置与 `MarshalJSON`）、`example/config.json`
- **Xboard 插件** `plugins/AccessLogAnalysis/`：路由、控制器、服务、`database/migrations/`、后台页面、`config.json`、`README.md`
- **依赖**：`golang.org/x/net/publicsuffix`（`golang.org/x/net` 已在依赖图中，从 indirect 变为 direct）；面板侧无新依赖。
- **性能**：每条连接一次登记/注销；每次读写多一次原子加法。面板每节点每 5 分钟一次批量写入。
- **兼容性**：节点开关默认关闭，未开启时行为与现在完全一致；面板未升级插件时节点收到 404 自动停用，不影响用户流量上报。用户计费口径（`u`/`d`、倍率）不变。
