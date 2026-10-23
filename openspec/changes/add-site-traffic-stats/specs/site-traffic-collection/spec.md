## ADDED Requirements

### Requirement: 按节点开启的站点流量采集
系统 SHALL 提供节点配置 `Options.SiteTraffic`，字段包括 `Enable`（默认 `false`）、`ReportInterval`（秒，默认 `300`，最小 `60`）、`Granularity`（`"site"` 或 `"host"`，默认 `"site"`）、`MaxSitesPerUser`（默认 `200`）。`Enable` 为 `false` 时，系统 MUST NOT 登记连接、创建采集器或调用站点流量接口。`SiteTraffic` 不为零值时，`NodeConfig.MarshalJSON` MUST 将其输出。

#### Scenario: 默认关闭
- **WHEN** 节点配置中没有 `SiteTraffic` 字段
- **THEN** 不为该节点 tag 创建站点采集器
- **AND** 不向 `/api/v1/server/site-traffic/push` 发送任何请求

#### Scenario: 上报间隔低于下限
- **WHEN** `SiteTraffic.ReportInterval` 配置为 `10`
- **THEN** 系统按 `60` 秒执行，并输出一条 Warn 日志

#### Scenario: 配置写回后开关不丢失
- **WHEN** 已有节点配置了 `SiteTraffic.Enable = true`，随后执行 `V2bX config add-node` 新增另一个节点
- **THEN** 写回的文件中原节点仍保留 `SiteTraffic.Enable = true`

### Requirement: 用户计费计数不受影响
无论是否开启站点流量采集，系统 SHALL 保证用户上传、下载计数，以及由此生成的 `UniProxy/push` 上报逐字节一致。在 Xray 上，`*dispatcher.SizeStatWriter` MUST 仍是上下行两条链路的最外层 writer，使 splice 零拷贝路径继续累加到用户计数。

#### Scenario: 开关前后用户合计一致
- **WHEN** 同样的流量分别在关闭和开启采集的情况下各转发一次
- **THEN** 两次 `GetUserTrafficSlice` 返回的每个用户上传、下载值完全相同

#### Scenario: splice 路径同时计入两处
- **WHEN** 在开启采集的连接上通过 `SizeStatWriter.Counter.Add(n)` 累加字节
- **THEN** 用户计数与该连接的站点计数都增加 `n`

### Requirement: sing-box 连接计数累加而非覆盖
`ConnCounter.Read` 与 `ConnCounter.Write` MUST 把本次读写的字节数累加到用户计数，MUST NOT 覆盖已有值。

#### Scenario: 两次读取累加
- **WHEN** 同一连接上 `Read` 先返回 100 字节、再返回 200 字节
- **THEN** 该用户的上传计数共增加 300

### Requirement: 流量归属到最终路由目标
系统 SHALL 按连接的最终路由目标确定站点：Xray 上，RouteOnly 嗅探时取路由目标，否则取嗅探覆盖后的目标地址；sing-box 上取嗅探得到的 `Domain`，为空时取目标地址；Hysteria2 的 TCP 流取 `HookedReqAddr`，为空时取 `ReqAddr`。绑定站点之前（例如嗅探期间）传输的字节 MUST 在绑定后计入同一站点。

#### Scenario: 嗅探到 TLS 域名
- **WHEN** 用户向一个 IP 发起 TLS 连接，嗅探到 SNI 为 `rr3---sn-abc.googlevideo.com`
- **THEN** 该连接的字节（包括 ClientHello）计入 `googlevideo.com`

#### Scenario: 没有域名
- **WHEN** 访问 `142.250.72.14:443` 的连接没有嗅探到域名
- **THEN** 其字节计入 `142.250.72.14`

#### Scenario: 被审计规则拒绝
- **WHEN** 连接在绑定站点之前因命中域名或协议审计规则被关闭
- **THEN** 它已传输的字节计入 `(unknown)`

### Requirement: 站点归并规则
系统 SHALL 按以下规则归并目标：主机名转小写、去掉末尾的 `.`、去掉端口；IP 地址原样保留（IPv6 不带方括号）。`Granularity = "site"` 时，域名 MUST 依据公共后缀列表归并为可注册域名，归并失败时保留完整主机名。`Granularity = "host"` 时 MUST 保留完整主机名。

#### Scenario: 多级公共后缀
- **WHEN** `Granularity = "site"`，目标为 `www.bbc.co.uk:443`
- **THEN** 站点为 `bbc.co.uk`

#### Scenario: 主机名粒度
- **WHEN** `Granularity = "host"`，目标为 `api2.cursor.sh:443`
- **THEN** 站点为 `api2.cursor.sh`

#### Scenario: IPv6 目标
- **WHEN** 目标为 `[2001:db8::1]:443`
- **THEN** 站点为 `2001:db8::1`

### Requirement: 周期增量采集
系统 SHALL 以「距上次采集的增量」方式采集站点流量，使长连接在每个有数据传输的上报周期中都有贡献。每次采集 MUST 返回 `(uid, site, upload, download, connections)` 条目，其中 `connections` 在连接绑定后的第一次采集中计 1 次。仍在嗅探中的连接 MUST 留到下一次采集；已关闭的连接 MUST 在取走最后一次增量后移除。

#### Scenario: 长连接跨周期
- **WHEN** 一条连接在第 1 个周期传输 1 GB、第 2 个周期传输 2 GB
- **THEN** 第 1 个周期该站点上报 1 GB，第 2 个周期上报 2 GB
- **AND** 该连接只在第 1 个周期计入 `connections`

#### Scenario: 已关闭连接被释放
- **WHEN** 连接关闭后又执行了一次采集
- **THEN** 采集器不再持有该连接的引用

### Requirement: Xray 连接结束判定
在 Xray 上，已登记的连接 SHALL 在连接上下文被取消，或上、下行两个 writer 都已关闭时（以先到者为准）视为结束。通过 writer 关闭结束时，MUST 撤销注册在上下文上的回调。

#### Scenario: mux 子流结束而主连接未断
- **WHEN** mux 子流结束并关闭了两个 writer，而主 mux 连接仍保持打开
- **THEN** 该子流视为已结束，并在下一次采集后被移除

### Requirement: Hysteria2 流与 UDP 归属
在 Hysteria2 上，系统 SHALL 在 `TraceStream` 到 `UntraceStream` 之间跟踪每条 TCP 流，以 `StreamStats.Tx` 为上传、`StreamStats.Rx` 为下传。对每个用户，`LogTraffic` 上报的字节中未被已跟踪 TCP 流覆盖的部分 MUST 计入 `(udp)`，结果小于 0 时取 0。

#### Scenario: 同时使用 TCP 和 UDP 的用户
- **WHEN** 某用户一个周期内 `LogTraffic` 合计 10 MB，其 TCP 流合计 7 MB
- **THEN** 该用户有 3 MB 计入 `(udp)`

### Requirement: 单用户站点数上限
系统 SHALL 在每次采集中为每个用户最多上报 `MaxSitesPerUser` 个站点。超出部分 MUST 按流量从小到大合并为一条 `(other)`，保证该用户的流量合计不变。

#### Scenario: 站点过多
- **WHEN** `MaxSitesPerUser = 200`，某用户一次采集中有 250 个站点
- **THEN** 上报中包含该用户流量最大的 199 个站点，以及一条承载其余流量的 `(other)`

### Requirement: 可靠且幂等的上报
系统 SHALL 把每次采集结果发送到 `POST /api/v1/server/site-traffic/push`，请求带节点 API 查询参数，以及唯一的 `batch_id`、`period_start`、`period_end` 和 `items`（每项为 `[uid, site, upload, download, connections]`）。单个请求 MUST 不超过 5000 条，超出 MUST 拆成多个批次。失败的批次 MUST 保留原 `batch_id`，并在新批次之前重试；队列最多 12 批，满了 MUST 丢弃最旧的批次并输出 Warn 日志。节点用户列表中不存在的用户 MUST 跳过。

#### Scenario: 面板暂时不可用
- **WHEN** 上报因网络错误或 5xx 响应失败
- **THEN** 下一轮先以相同 `batch_id` 重发该批次，再发送新批次

#### Scenario: 采集结果过大
- **WHEN** 一次采集产生 12000 条
- **THEN** 发送 3 个 `batch_id` 各不相同的批次，每批不超过 5000 条

### Requirement: 面板缺少接口时平稳降级
站点流量接口返回 HTTP 404 时，系统 SHALL 清空队列、在该节点重新加载前停止站点流量上报，并只输出一次 Warn 日志。用户流量上报 MUST 不受影响。

#### Scenario: 面板未安装插件
- **WHEN** 面板对第一次站点流量上报返回 404
- **THEN** 该节点重新加载之前不再发送站点流量请求
- **AND** `UniProxy/push` 按原有周期继续运行
