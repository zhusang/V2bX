## ADDED Requirements

### Requirement: 节点鉴权的上报接口
`AccessLogAnalysis` 插件 SHALL 提供 `POST /api/v1/server/site-traffic/push`，由核心 `server` 中间件保护。服务器 ID MUST 取自中间件解析出的节点，MUST NOT 取自请求体。接口 MUST 校验：`batch_id` 为不超过 64 字符的字符串；`period_start`、`period_end` 为整数；每一项为 `[uid:整数, site:字符串 ≤ 253, upload:整数 ≥ 0, download:整数 ≥ 0, connections:整数 ≥ 0]`；最多 5000 项。

#### Scenario: 合法上报
- **WHEN** 节点以正确的 `server_token`、`node_id`、`node_type` 发送合法批次
- **THEN** 插件按该节点保存数据并返回 HTTP 200

#### Scenario: Token 错误
- **WHEN** 请求携带错误的 `token`
- **THEN** 核心中间件拒绝请求，不写入任何数据

#### Scenario: 批次过大
- **WHEN** 请求包含 5001 项
- **THEN** 插件返回 HTTP 422，不写入任何数据

### Requirement: 批次幂等处理
插件 SHALL 按服务器记录已成功入库的 `batch_id`，保留 24 小时。同一服务器上已入库的 `batch_id` 再次到达时，MUST 直接返回成功，不写入任何数据。

#### Scenario: 响应丢失后重试
- **WHEN** 同一服务器的同一批次被接收两次
- **THEN** 流量合计与只接收第一次时相同

### Requirement: 按天汇总存储
插件 SHALL 将站点流量存入 `v2_site_traffic`，该表由插件 `database/migrations` 目录下的迁移创建，每个 `(user_id, server_id, site, record_date)` 一行。新到数据 MUST 累加到该行已有的 `upload`、`download`、`conn_count`。字节数 MUST 按节点实际转发值存储，不乘服务器倍率。`record_date` MUST 由 `period_end` 按面板时区换算；`period_end` 与面板当前时间相差超过 10 分钟时，MUST 改用接收时间。写入 MUST 批量执行，并支持 MySQL、PostgreSQL、SQLite。

#### Scenario: 同一天内累加
- **WHEN** 同一天两个批次分别上报用户 123 经服务器 21 访问 `googlevideo.com` 下载 1 GB 和 2 GB
- **THEN** 该键对应的唯一一行 `download` 为 3 GB

#### Scenario: 节点时钟漂移
- **WHEN** 批次的 `period_end` 比面板时间快 2 小时
- **THEN** 该批次记到面板当前日期

#### Scenario: 写库失败
- **WHEN** 数据库写入失败
- **THEN** 插件返回 HTTP 500，且不把该 `batch_id` 标记为已处理

### Requirement: 后台站点流量查询
插件 SHALL 提供后台接口（Sanctum 鉴权，仅管理员或员工可用），包括：某用户的分站点流量（按用户 ID 或邮箱）、按总流量排序的网站排行（附去重用户数）、某网站下按流量排序的用户、存储统计。`days` MUST 限制在 1–90，`limit` MUST 限制在 1–200。用户明细 MUST 同时返回该用户同期来自 `v2_stat_user` 的计费流量，并标明其已乘倍率。

#### Scenario: 用户流量去向
- **WHEN** 管理员查询用户 `a@example.com` 近 7 天
- **THEN** 返回按上传加下载降序的站点列表，每项含上传、下载、连接数及占该用户站点合计的比例
- **AND** 同时返回该用户同期的计费上传与下载

#### Scenario: 某网站的头部用户
- **WHEN** 管理员查询 `googlevideo.com` 近 3 天、limit 为 20
- **THEN** 返回在该网站流量最大的至多 20 个用户

#### Scenario: 非管理员访问
- **WHEN** 既不是管理员也不是员工的用户调用上述任一接口
- **THEN** 插件返回 HTTP 403

### Requirement: 保留期清理覆盖站点流量
插件的定时清理与手动清理 SHALL 按访问日志使用的同一保留天数，分批删除 `record_date` 早于保留期的 `v2_site_traffic` 行。

#### Scenario: 每日清理
- **WHEN** 保留天数为 3 且清理任务执行
- **THEN** 3 天前的站点流量行被删除，更新的行保留

### Requirement: 后台页面展示站点流量
插件后台页面 SHALL 新增「网站流量」视图，包含用户查询、网站排行和「网站 → 用户」下钻，流量以易读单位显示。原有的访问次数视图 MUST 保留并标注为弃用。

#### Scenario: 从排行下钻
- **WHEN** 管理员在网站排行中点击某个网站
- **THEN** 页面显示所选时段内该网站的头部用户

### Requirement: 旧日志解析脚本不含凭据
`plugins/AccessLogAnalysis/scripts/` 下的脚本 SHALL 使用占位符，不包含真实的面板地址、API Token 或服务器 ID。

#### Scenario: 检查仓库
- **WHEN** 检查这些脚本
- **THEN** `XBOARD_API`、`API_TOKEN`、`SERVER_ID` 均为占位值
