# Change: 新增 CLI 配置增量管理命令

## Why
目前 V2bX 的配置文件只能整体创建/覆盖，无法增量修改。用户若想新增一个节点，必须把整个配置文件（包括所有已有节点和 Core 配置）重新写一遍，操作繁琐且容易出错。需要提供 CLI 命令来支持配置的增量读取和修改。

## What Changes
- 在 `conf` 包新增 `Save()` 方法，支持将当前配置序列化并写回 JSON 文件
- 新增 `cmd/config.go`，提供 `V2bX config` 子命令组：
  - `V2bX config show` — 查看当前完整配置
  - `V2bX config list-nodes` — 列出所有已配置的节点（简洁表格）
  - `V2bX config add-node` — 通过命令行参数添加一个新节点到现有配置
  - `V2bX config del-node <index>` — 按索引删除一个节点
- 所有修改操作均为：读取现有配置 → 增量修改 → 写回文件，不影响其他配置项

## Impact
- Affected specs: config-management（新增 capability）
- Affected code:
  - `conf/conf.go` — 新增 Save 方法
  - `cmd/config.go` — 新增文件，配置管理命令
