## Context

### 现在的流量是怎么算出来的

```
                  V2bX 节点                                           Xboard 面板
┌─────────────────────────────────────────────┐      ┌──────────────────────────────────────┐
│ 连接字节 ──► 用户计数器 TrafficStorage        │      │ UniProxyController::push             │
│   Xray : SizeStatWriter / CounterReader      │      │   └─ UserService::trafficFetch       │
│   sing : ConnCounter / PacketConnCounter     │ push │       ├─ TrafficFetchJob             │
│   hy2  : HookServer.LogTraffic(id, tx, rx)   │─────►│       │   u += up × 倍率, d += down × 倍率│
│                                              │ uid→ │       ├─ StatUserJob  (v2_stat_user) │
│ GetUserTrafficSlice(reset) 每 PushInterval    │[u,d] │       └─ StatServerJob(v2_stat_server)│
└─────────────────────────────────────────────┘      └──────────────────────────────────────┘
```

- 计数按「节点 tag → 用户」聚合，连接一旦写进用户计数器就丢失了目标信息。
- Xray 的计数器在 `getLink()` 里装上，但目标域名要等嗅探完成、进入 `routedDispatch()` 才最终确定。也就是说**计数开始时还不知道网站**，必须先计数、后归属。
- Xray 的 XTLS splice 零拷贝路径（`proxy.CopyRawConnIfExist`）只认最外层的 `*dispatcher.SizeStatWriter`，从它的 `Counter` 记用户流量。任何包在 `SizeStatWriter` 外面的新 writer 都会让 splice 流量漏计，直接造成**计费错误**。
- Xray inbound worker 在 TCP/UDP 连接结束时取消该连接的 `ctx`；但 mux 子流与主连接共用同一个 `ctx`（`common/mux/server.go` 直接把父 `ctx` 传给 `Dispatch`），子流结束不会单独取消。
- sing-box 的 `RoutedConnection` / `RoutedPacketConnection` 在路由完成后才回调，此时 `InboundContext` 里已有嗅探到的 `Domain` 和最终 `Destination`。
- Hysteria2 的 `TrafficLogger` 有 `TraceStream(stream, *StreamStats)` / `UntraceStream`。每条 TCP 流的 `StreamStats` 带 `AuthID`、`ReqAddr`、`HookedReqAddr` 和实时累加的 `Tx`/`Rx`。UDP 只走 `LogTraffic(id, tx, rx)`，没有目标地址。
- Xray 入站默认嗅探 `http`、`tls` 并覆盖目标地址，所以 TCP 上的 HTTP/HTTPS 能拿到域名；QUIC 不在嗅探列表里，UDP 443 只能看到 IP。

### 约束

- 用户计费口径（`u`/`d`、节点倍率、`UniProxy/push`）**一字节都不能变**。
- 节点上是热路径：每次读写只允许增加常数级的原子操作，不能加锁。
- 需要兼容面板未升级、插件未安装的情况。
- V2bX 的 `NodeConfig.MarshalJSON` 是手写字段映射，新增配置项必须同步加进去，否则 `config add-node` 写回时会丢失。

### 相关方

- 运营/客服：回答「我的流量去哪了」。
- 管理员：发现滥用（如 P2P、某站点刷流量）。
- 节点维护者：关心 CPU、内存开销和稳定性。

## Goals / Non-Goals

**Goals：**

- 在 Xray、sing-box、Hysteria2 三种内核上，按「用户 × 站点」采集上行字节、下行字节、连接数。
- 长连接（视频、下载）跨上报周期、跨天时，流量按实际传输的时段归属，而不是全部记在连接结束那一刻。
- 节点到面板的上报可重试且幂等：失败不丢、重试不重复计数。
- 面板后台能按用户、按网站、按「网站下的用户」查看流量排行。
- 开关默认关闭；关闭时零额外开销、行为与现状完全一致。

**Non-Goals：**

- 不用站点流量做计费或限额，计费仍以 `UniProxy/push` 为准。
- 不追求站点合计与用户计费流量逐字节相等（采集时刻不同、倍率不同，见 Decision 7）。
- 不解析 IP 归属（反查 PTR、ASN、GeoIP 都不做）。没嗅探到域名的连接就显示 IP。
- 不改 Xray 默认嗅探列表（加 `quic` 会改变路由行为，留给运维按需开启）。
- 不拆分 Hysteria2 的 UDP 流量。
- 不做实时视图（数据延迟以上报周期计，默认 5 分钟）。

## Decisions

### Decision 1：「同时累加」计数器，而不是外包一层 writer

在已有用户计数器上加一个可选的第二目标：

```go
type XrayTrafficCounter struct {
    V     *atomic.Int64 // 用户计数，原样保留
    Extra *atomic.Int64 // 站点计数，可为 nil
}

func (c *XrayTrafficCounter) Add(delta int64) int64 {
    if c.Extra != nil {
        c.Extra.Add(delta)
    }
    return c.V.Add(delta)
}
```

`CounterReader` 同样增加 `Extra`。`SizeStatWriter` 仍是最外层，splice 路径通过 `statWriter.Counter.Add(w)` 会同时记到用户和站点。

**放弃的方案**：再包一层 `SiteStatWriter`。实现最直观，但 splice 路径的类型断言会失败，用户计费流量直接漏计，不可接受。

sing-box 的 `ConnCounter` / `PacketConnCounter` 同理：`Read/Write/ReadBuffer/WriteBuffer` 和 `UnwrapReader/UnwrapWriter` 返回的 `CountFunc` 里都同时累加两处。sing 的 `bufio.Copy` 快路径依赖 `CountFunc`，漏掉就会漏计。

**顺带修复**：`ConnCounter.Read/Write` 当前写的是 `UpCounter.Store(int64(n))` / `DownCounter.Store(int64(n))`，会把用户累计值覆盖成最后一次读写的字节数，改为 `Add`。这是现有的计费 bug，和本变更同一处代码，一并修掉并补单测。

### Decision 2：连接登记 + 周期增量采集，而不是「连接关闭时记一笔」

新包 `common/sitestat`：

```go
type Conn struct {
    Up, Down atomic.Int64          // 未采集的增量
    user     string                // 内核里的用户键（Xray 为 email，sing/hy2 为 uuid）
    site     atomic.Pointer[string] // nil 表示尚未归属
    counted  atomic.Bool           // 连接数是否已计入
    state    atomic.Uint32         // 上行关闭 / 下行关闭 / 已关闭 位标记
    stop     func() bool           // 撤销 context.AfterFunc
}

type Collector struct {
    shards [16]struct {
        sync.Mutex
        live map[*Conn]struct{}
    }
    opt Options
}

func (c *Collector) Open(user string) *Conn
func (x *Conn) Bind(host string) // 归并为站点后写入 site
func (x *Conn) CloseHalf(dir)    // 两个方向都关闭 → Close
func (x *Conn) Close()
func (c *Collector) Harvest() []Entry // Entry{User, Site, Up, Down, Conns}
```

`Harvest` 对每个存活连接执行：

| 连接状态 | 处理 |
|---|---|
| 已归属 | `Swap(0)` 取出上下行增量，加到 `(user, site)`；首次被采集时连接数加 1 |
| 未归属、未关闭（还在嗅探） | 跳过，字节留到下一轮 |
| 未归属、已关闭（被审计规则拒绝或嗅探前断开） | 归入 `(unknown)` |
| 已关闭 | 本轮取完增量后从登记表移除 |

**为什么不在关闭时记一笔**：一个 3 小时的视频连接，会把几个 G 全部记在关闭那一刻，跨天时日期也会错；节点重启时未关闭的连接会整笔丢失。周期增量采集只依赖「当前还有多少没取走」，与连接寿命无关。

**为什么分片**：`Open/Close` 每条连接各一次，需要加锁；按 `*Conn` 指针哈希分 16 片，避免高并发建连时争用同一把锁。读写热路径只有原子加法，不加锁。

### Decision 3：Xray 的绑定点与关闭判定

- **登记**：`getLink()` 和 `DispatchLink()` 中已有 `user != nil` 分支，在这里 `Open(user.Email)`，并把 `Conn.Up` / `Conn.Down` 作为 `Extra` 传给计数器。
- **绑定**：`routedDispatch()` 增加 `*sitestat.Conn` 参数。在审计规则（`CheckDomainRule` / `CheckProtocolRule`）通过之后、`handler.Dispatch` 之前调用 `Bind`。目标取值：`ob.RouteTarget` 有效时（RouteOnly 模式）用它，否则用已被嗅探覆盖的 `destination`。被规则拒绝的分支直接 `Close()`。
- **关闭判定**，以先到者为准：
  1. `context.AfterFunc(ctx, conn.Close)`：覆盖普通 TCP/UDP 连接；
  2. 在 `SizeStatWriter` **内侧**包一个只拦截 `Close()` 的 writer，上行、下行都关闭后视为结束。这条覆盖 mux 子流（共享父 `ctx`）。结束时调用 `stop()` 撤销 AfterFunc，避免长寿命 mux 连接上堆积回调。

  `DispatchLink` 只有一个出站 writer，用 1 加这个 writer 的关闭判定。

- **未开启时**：`Collector` 为 nil，`Open` 返回 nil，`Extra` 为 nil，所有分支只多一次 nil 判断。

### Decision 4：sing-box 与 Hysteria2 的接入

- **sing-box**：在 `RoutedConnection` / `RoutedPacketConnection` 现有检查通过后 `Open(m.User)`，并立即 `Bind`。站点取 `m.Domain`（嗅探结果），为空时取 `m.Destination.AddrString()`。关闭判定通过 `ConnCounter.Close()` 覆写。UDP 只按首个目标归属（同一个 PacketConn 发往多个目标的情况较少，记为已知限制）。
- **Hysteria2**：
  - `TraceStream(stream, stats)` 登记 `{user: stats.AuthID, stats, lastTx, lastRx}`；
  - `Harvest` 读取 `stats.Tx/Rx`，减去上次值得到增量；
  - 站点取 `HookedReqAddr`（嗅探后地址），为空时取 `ReqAddr`，并去掉端口；
  - `UntraceStream` 标记关闭。
  - 方向：`copyTwoWayEx` 中客户端到远端累加 `Tx`，远端到客户端累加 `Rx`，因此 `Tx` 为上行、`Rx` 为下行，与现有 `LogTraffic` 用法一致。
  - UDP：`LogTraffic` 在 TCP 流和 UDP 消息上**都会**被调用，钩子里分不清来源。做法是在 `LogTraffic` 中另记一份「用户全量」增量，`Harvest` 时用它减去该用户所有 TCP 流的增量，差值（截断为 ≥ 0）记到 `(udp)`。这样站点合计与用户计数基本对齐，不至于缺掉 UDP 那一块。

### Decision 5：站点归并在节点上做

| 原始目标 | `Granularity: "site"`（默认） | `Granularity: "host"` |
|---|---|---|
| `rr3---sn-oguesnzz.googlevideo.com:443` | `googlevideo.com` | `rr3---sn-oguesnzz.googlevideo.com` |
| `www.bbc.co.uk:443` | `bbc.co.uk` | `www.bbc.co.uk` |
| `142.250.72.14:443` | `142.250.72.14` | `142.250.72.14` |
| `[2001:db8::1]:443` | `2001:db8::1` | `2001:db8::1` |

- 用 `golang.org/x/net/publicsuffix.EffectiveTLDPlusOne`，失败时回退为原主机名；统一转小写、去掉末尾的 `.`；端口一律丢弃。
- **为什么在节点做**：直接压缩基数。一个看视频的用户一小时可能连几十个 `googlevideo.com` 子域名，归并后只剩 1 条。PHP 侧没有现成的公共后缀库，引入会给 Xboard 核心加依赖。
- **基数保护**：每轮每个用户最多 `MaxSitesPerUser`（默认 200）个站点，超出的按流量从小到大合并进 `(other)`。

### Decision 6：上报协议与可靠性

```http
POST {ApiHost}/api/v1/server/site-traffic/push?node_type=vless&node_id=21&token=***
Content-Type: application/json

{
  "batch_id": "0d6c2f0e-5b8b-4a0b-9d0e-5f6a4b1f7c21",
  "period_start": 1761206400,
  "period_end": 1761206700,
  "items": [
    [123, "googlevideo.com", 1048576, 52428800, 37],
    [123, "(unknown)", 2048, 0, 3]
  ]
}
```

- `items` 每项为 `[uid, site, upload, download, conns]`，用数组而不是对象，体积约减半，风格与 `UniProxy/push` 一致。
- **鉴权**：沿用 V2bX API 客户端已带的全局查询参数（`node_type`、`node_id`、`token`），面板用核心 `server` 中间件校验。不再有插件专用 Token；`server_id` 取自中间件解析出的节点，节点不能冒充其他节点。
- **独立任务**：`siteTrafficReportPeriodic`，默认 300 秒、最小 60 秒，不与 `PushInterval` 绑定，避免把面板写入频率拉到每分钟。
- **单请求上限**：5000 条，超出拆成多批，每批独立 `batch_id`。
- **失败重试**：失败批次进入内存队列，原样保留 `batch_id`，下一轮先发旧批次。队列上限 12 批（默认约 1 小时），超出丢弃最旧批次并打 Warn 日志。
- **幂等**：面板写库成功后 `Cache::put("site_traffic_batch:{server_id}:{batch_id}", 1, 86400)`；收到已处理的 `batch_id` 时直接返回成功。用于覆盖「面板已写入但响应丢失，节点重试」的情况。
- **面板未安装插件**：收到 404 时清空队列，并在本次节点生命周期内停用站点上报，只打一次 Warn。节点配置重新加载（`nodeInfoMonitor` 检测到节点变化）后会重新尝试。
- **重启丢失**：队列只在内存中，节点重启会丢掉未发送的数据（最多一个上报周期加队列中的批次）。这是分析数据，不做落盘。

### Decision 7：面板存原始字节，不乘倍率

- `v2_site_traffic` 存的是节点实际转发的字节，不乘节点倍率。
- **原因**：倍率是计费策略，还可能按时段变化；站点分析要的是物理用量。若存折算值，同一网站在倍率 0.5 和 2 的节点上会出现 4 倍差距，排行失真。
- 后台用户明细页同时展示该用户同期的**计费流量**（来自 `v2_stat_user`，已乘倍率）作为参照，并注明两者口径不同。
- 站点合计与计费流量对不上的来源：倍率、`(unknown)` 和 `(other)` 的合并、两个上报任务的采集时刻不同、节点重启丢失。

### Decision 8：面板数据模型与写入

插件自带迁移 `plugins/AccessLogAnalysis/database/migrations/`，安装或启用时由 `PluginManager::runMigrations` 自动执行：

```text
v2_site_traffic
  id            bigint unsigned PK
  user_id       int unsigned
  server_id     int unsigned
  site          varchar(253)
  upload        bigint unsigned default 0
  download      bigint unsigned default 0
  conn_count    int unsigned default 0
  record_date   date
  created_at    int unsigned
  updated_at    int unsigned

  UNIQUE uk_user_server_site_date (user_id, server_id, site, record_date)
  INDEX  idx_date_site            (record_date, site)
  INDEX  idx_user_date            (user_id, record_date)
```

- **日期**：取 `period_end`，按面板时区换算；与面板当前时间相差超过 10 分钟时改用接收时间，防止节点时钟漂移。重试批次仍按原 `period_end` 归属，所以跨零点重试不会记错日期。
- **写入**：请求内同步写入，每 500 行一次批量 upsert，增量累加 `upload`、`download`、`conn_count`。MySQL、PostgreSQL、SQLite 三种写法参照核心 `StatUserJob`。同步写入的好处是失败时直接返回 500，由节点重试；放进队列的话，任务失败就丢了。
- **不校验 uid 是否存在**：uid 来自面板下发的用户列表，逐条校验代价高；已删除用户的残留行由保留期清理。
- **数据量估算**：5000 个活跃用户 × 平均 60 个站点 × 平均 1.5 个节点 ≈ 45 万行/天。默认保留 3 天约 135 万行，单行约 100 字节，含索引约 300 MB。保留天数上限 90 天时需要评估磁盘，后台配置页会显示当前行数。

### Decision 9：后台查询

所有查询都限定 `record_date >= 起始日期`，命中上面的索引。

| 接口 | 用途 | 聚合方式 |
|---|---|---|
| `GET .../site-traffic/user?email=` 或 `user_id=`，`days` | 某用户的网站流量明细 | `GROUP BY site`，按 `SUM(upload+download)` 降序，附占比与同期计费流量 |
| `GET .../site-traffic/ranking?days&limit` | 全站网站流量排行 | `GROUP BY site`，附用户数 |
| `GET .../site-traffic/site-users?site&days&limit` | 某网站下哪些用户用得最多 | `WHERE site=? GROUP BY user_id` |
| `GET .../site-traffic/stats` | 行数、最早/最新日期 | 供配置页展示 |

管理接口沿用插件现有的 Sanctum 鉴权与 `is_admin || is_staff` 判断。`days` 限制 1–90，`limit` 限制 1–200。插件后台页面新增「网站流量」标签页，旧的「访问次数」页保留。

### Decision 10：旧的 access log 方案降级为弃用

- 保留 `/api/plugin/access-log/report` 与 `v2_access_log`，避免已部署节点立刻报错。README 标注弃用，并说明迁移到节点内置采集。
- `scripts/log-parser*.sh` 里硬编码了线上 `XBOARD_API` 和 `API_TOKEN`：改为占位符，并在发布说明里要求**立即在后台重新生成 Token**。

## Risks / Trade-offs

- [新计数逻辑写错，影响用户计费] → 用户计数路径只增加 `Extra` 分支，不改原有 `V.Add`；新增单测断言开启和关闭站点采集时用户计数完全相同，覆盖 splice 路径的 `SizeStatWriter.Counter`。
- [热路径开销] → 每次读写多一次原子加法；每条连接一次分片加锁登记和注销。实施时用 `go test -bench` 对比开启前后的转发吞吐，作为上线门槛：吞吐下降不超过 2%。
- [登记表泄漏，内存上涨] → 两条关闭路径（ctx 取消、双向 writer 关闭）任一到达即结束；另加兜底：连续 3 轮无增量且 `ctx.Err() != nil` 的连接强制移除。暴露登记表大小的 Debug 日志。
- [域名拿不到] → QUIC（Xray 未嗅探）、纯 IP 直连、ECH 连接都会显示 IP。README 说明如何在 `XrayOptions` 里按需开启 `quic` 嗅探及其副作用。
- [CDN 共享域名归属模糊] → `cloudfront.net`、`akamaized.net` 这类站点无法对应到具体业务，这是 SNI 维度统计的固有限制。`host` 粒度可以辅助排查。
- [隐私与合规] → 这是按用户记录的浏览目标，属于敏感数据：默认关闭、默认保留 3 天、仅管理员和员工可查。README 提示运营者自行评估当地法规和服务条款。
- [面板写入压力] → 默认 5 分钟一批、单批 ≤ 5000 条、500 行一次 upsert。节点很多时可调大 `ReportInterval`。
- [mux / XUDP 子流的连接数偏大] → 每个子流算一个连接，「连接数」只用于参考，不作为指标。

## Migration Plan

1. **面板先行**：升级 `AccessLogAnalysis` 插件。启用时自动建表，新接口上线；旧接口继续可用。
2. **节点灰度**：升级 V2bX 二进制，开关默认关闭，行为不变。选一台节点在 `Options.SiteTraffic.Enable` 设为 `true`，观察一个上报周期后的 CPU、内存和面板数据，再逐步扩大。
3. **核对口径**：抽查若干用户，站点合计（原始字节）× 节点倍率 应与 `v2_stat_user` 同期计费流量大致相符（误差主要来自 `(unknown)` 和采集时刻）。
4. **下线旧方案**：所有节点切换后，删除节点 crontab 里的 `log-parser.sh`，并重新生成插件 Token 使旧 Token 失效。
5. **回滚**：节点把开关设回 `false`，或回退二进制，计费不受影响。面板插件回退版本时新表保留，不做破坏性回滚。

## Open Questions

- 后台是否需要提供「按倍率折算」的视图？当前倾向不需要：倍率会按时段变化，按天汇总后无法准确还原。
- 是否保留 `server_id` 维度？保留会让行数约乘以平均节点数，但能回答「哪个节点上的流量」。当前倾向保留。
- 是否给用户端开放「我的网站流量」？涉及隐私展示和成本，本变更不做，留作后续评估。
- 旧的 `v2_access_log` 迁移在 Xboard 核心的 `database/migrations/` 里，而不在插件目录下。是否在后续版本移入插件，需要和 Xboard 主仓库的升级流程一起决定。

## 附：实施中发现的相关问题

- `common/counter/conn.go`：`ConnCounter.Read/Write` 用 `Store` 覆盖用户计数（Decision 1 已纳入修复）。
- `node/task.go` 第 162 行：PushInterval 变化时把 `userReportPeriodic.Interval` 设成了 `newN.PullInterval`，应为 `PushInterval`。不在本变更范围内，单独修复。
