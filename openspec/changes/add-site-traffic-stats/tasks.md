## 1. 基线与计费保护

- [x] 1.1 在 `common/counter/` 新增单测：分别通过 `Read`、`Write`、`ReadBuffer`、`WriteBuffer`、`UnwrapReader/UnwrapWriter` 返回的 `CountFunc` 写入字节，断言用户计数为累加值（先复现 `Store` 覆盖 bug）
- [x] 1.2 修复 `ConnCounter.Read/Write`：`Store(int64(n))` 改为 `Add(int64(n))`，1.1 用例转为通过
- [x] 1.3 在 `core/xray/app/dispatcher/` 新增基线单测：构造带用户的 `getLink`，经 `SizeStatWriter.Counter.Add` 与普通 `WriteMultiBuffer` 两条路径写入，记录用户计数；后续每个任务都要保持该用例结果不变

## 2. 配置项

- [x] 2.1 在 `conf/node.go` 新增 `SiteTrafficConfig{Enable, ReportInterval, Granularity, MaxSitesPerUser}` 并挂到 `Options.SiteTraffic`，实现默认值与 `ReportInterval < 60` 时取 60 并输出 Warn
- [x] 2.2 在 `NodeConfig.MarshalJSON` 中输出非零值的 `SiteTraffic`；在 `conf/node_marshal_test.go` 增加往返用例
- [x] 2.3 在 `example/config.json` 的节点示例中加入注释掉的 `SiteTraffic` 配置及说明

## 3. 采集器 `common/sitestat`

- [x] 3.1 实现 `Normalize(target, granularity)`：转小写、去末尾点、去端口、IPv6 去方括号，`site` 粒度用 `publicsuffix.EffectiveTLDPlusOne`，失败回退为主机名；把 `golang.org/x/net` 改为 direct 依赖
- [x] 3.2 为 `Normalize` 写表驱动单测，覆盖 `googlevideo.com` 子域、`bbc.co.uk`、IPv4、IPv6、带点结尾、纯 TLD、空字符串
- [x] 3.3 实现 `Conn`（`Up/Down` 原子计数、`Bind`、`CloseHalf`、`Close`、`stop` 回调撤销）与 16 分片的 `Collector.Open`
- [x] 3.4 实现 `Collector.Harvest`：已绑定取增量、未绑定未关闭跳过、未绑定已关闭归 `(unknown)`、关闭后移除、首次采集计连接数
- [x] 3.5 实现 `MaxSitesPerUser` 截断，超出部分按流量从小到大合并为 `(other)`，且用户合计不变
- [x] 3.6 实现按节点 tag 的注册表（创建、获取、删除），`Enable=false` 时返回 nil，所有调用点对 nil 安全
- [ ] 3.7 单测：长连接跨两次采集、嗅探中跨采集、关闭后释放、并发 `Open/Close/Harvest` 跑 `-race`

## 4. Xray 接入

- [x] 4.1 `XrayTrafficCounter` 与 `CounterReader` 增加可为 nil 的 `Extra *atomic.Int64`，`Add` 同时累加两处
- [x] 4.2 `DefaultDispatcher` 增加按 tag 获取采集器的方法；在 `getLink()` 与 `DispatchLink()` 的用户分支中 `Open(user.Email)`，并把 `Conn.Up/Down` 作为 `Extra` 传入
- [x] 4.3 在 `SizeStatWriter` 内侧包一个只拦截 `Close()` 的 writer，上、下行都关闭后调用 `Conn.Close()`；在 `Dispatch`/`DispatchLink` 注册 `context.AfterFunc(ctx, conn.Close)`，并保存其撤销函数
- [x] 4.4 `routedDispatch` 增加 `*sitestat.Conn` 参数：审计规则拒绝的分支调用 `Close()`；通过后在 `handler.Dispatch` 前按 `ob.RouteTarget`（有效时）或 `destination` 调用 `Bind`
- [x] 4.5 在 `core/xray/user.go` 实现 `GetSiteTrafficSlice(tag)`：调用 `Harvest`，经 `uidMap` 把 email 转为 uid，跳过未知用户
- [x] 4.6 `AddNode`/`DelNode` 时创建、销毁该 tag 的采集器
- [x] 4.7 单测：splice 路径（直接调用 `SizeStatWriter.Counter.Add`）同时计入用户与站点；1.3 基线结果不变；mux 场景下子流关闭两个 writer 后被移除

## 5. sing-box 接入

- [x] 5.1 `NewConnCounter` / `NewPacketConnCounter` 增加可为 nil 的 `*sitestat.Conn` 参数，所有计数路径（含 `CountFunc`）同时累加；覆写 `Close()` 调用 `Conn.Close()`
- [x] 5.2 在 `RoutedConnection` / `RoutedPacketConnection` 现有检查通过后 `Open(m.User)` 并以 `m.Domain`（为空则 `m.Destination.AddrString()`）`Bind`
- [x] 5.3 在 `core/sing/user.go` 实现 `GetSiteTrafficSlice`，并在节点增删时维护采集器
- [x] 5.4 单测：TCP、UDP 各一条连接的站点归属与用户计数一致

## 6. Hysteria2 接入

- [x] 6.1 实现 `HookServer.TraceStream`：登记 `{AuthID, *StreamStats, lastTx, lastRx}`；`UntraceStream` 标记关闭
- [x] 6.2 在 `LogTraffic` 中按用户另记一份全量增量
- [x] 6.3 在 `core/hy2/user.go` 实现 `GetSiteTrafficSlice`：TCP 流以 `Tx/Rx` 差值按 `HookedReqAddr`/`ReqAddr` 归属；用户全量减去 TCP 流合计（≥ 0）计入 `(udp)`
- [x] 6.4 单测：模拟两条 TCP 流加 UDP 消息，断言各站点与 `(udp)` 的值，以及 `Tx` 为上行、`Rx` 为下行

## 7. 接口与上报任务

- [x] 7.1 `core.Core` 接口新增 `GetSiteTrafficSlice(tag string) ([]panel.SiteTraffic, error)`，`Selector` 按 tag 转发
- [x] 7.2 新增 `api/panel/site_traffic.go`：`SiteTraffic` 类型与 `ReportSiteTraffic(batch)`，请求体为 `{batch_id, period_start, period_end, items}`，404 时返回可识别的哨兵错误
- [x] 7.3 在 `node/` 新增上报任务：按 `ReportInterval` 采集，拆成每批 ≤ 5000 条并生成 `batch_id`，失败批次排队（≤ 12 批）优先重发，404 时清空队列并停用到节点重载，只打一次 Warn
- [x] 7.4 在 `startTasks` 中按开关启动任务，在节点重载和关闭时停止任务并清理队列
- [x] 7.5 单测：用 `httptest` 模拟面板，覆盖成功、5xx 重试复用 `batch_id`、队列满丢最旧、404 停用、`UniProxy/push` 不受影响

## 8. 面板插件：接收与存储

- [x] 8.1 在 `plugins/AccessLogAnalysis/database/migrations/` 新增 `v2_site_traffic` 迁移（唯一键与两个索引见设计）
- [x] 8.2 在 `routes/api.php` 注册 `POST api/v1/server/site-traffic/push`，挂核心 `server` 中间件
- [x] 8.3 新增 `SiteTrafficController::push`：参数校验、`batch_id` 去重、按 `period_end`（漂移超 10 分钟用接收时间）计算 `record_date`、调用服务写入、成功后写去重缓存
- [x] 8.4 新增 `SiteTrafficService::save`：每 500 行批量 upsert 累加，按 MySQL、PostgreSQL、SQLite 分别实现（参照 `StatUserJob`）
- [ ] 8.5 测试：合法批次、错误 Token、5001 条、重复 `batch_id`、同日累加、时钟漂移、写库失败返回 500 且不标记去重

## 9. 面板插件：查询、页面与清理

- [x] 9.1 在 `SiteTrafficService` 实现用户明细（附同期 `v2_stat_user` 计费流量）、网站排行、网站下的用户、存储统计
- [x] 9.2 在 `AdminController` 增加对应的四个接口，沿用 Sanctum 与 `is_admin || is_staff` 校验，`days`、`limit` 做范围限制
- [x] 9.3 `LogCleanupService` 增加对 `v2_site_traffic` 的分批清理，`getStorageStats` 返回新表统计
- [x] 9.4 后台页面新增「网站流量」标签页：用户查询、网站排行、点击网站下钻到用户，流量用 KB/MB/GB 显示；旧的访问次数页标注弃用
- [x] 9.5 插件 `config.json` 版本升到 2.0.0；README 增加节点开启说明、口径说明（原始字节、不乘倍率）、QUIC 嗅探说明和隐私提示

## 10. 清理旧方案凭据

- [x] 10.1 `scripts/log-parser*.sh` 中的 `XBOARD_API`、`API_TOKEN`、`SERVER_ID` 改为占位符
- [x] 10.2 在发布说明中要求运营者在后台重新生成 Token，使已泄露的旧 Token 失效

## 11. 验收

- [ ] 11.1 按 AGENTS.md 的构建命令（`GOEXPERIMENT=jsonv2` 与全部 tags）编译通过，`go test -race ./...` 通过
- [ ] 11.2 基准测试：对比开关关闭、开启时 Xray 与 sing-box 的转发吞吐，下降不超过 2%
- [ ] 11.3 在一台测试节点开启采集运行 24 小时：登记表大小平稳、无内存持续增长；抽查 3 个用户，站点合计 × 节点倍率与 `v2_stat_user` 同期计费流量偏差在 5% 以内
- [x] 11.4 把 `specs/site-traffic-panel/` 同步到 Xboard 仓库的 openspec
