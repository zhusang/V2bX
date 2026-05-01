## Why

当前 `V2bX config add-node`、`del-node` 等 CLI 命令以及任何调用 `conf.Save()` 的脚本，本质上是 **整文件覆盖写入**：先把整个 `Conf` 反序列化到结构体，再用 `MarshalJSON` + `os.WriteFile` 重写整个文件。

这种"覆盖式生成"在实际使用中暴露了三个明显问题：

1. **节点字段丢失**：`conf/node.go` 的 `NodeConfig.MarshalJSON` 只显式输出了 ApiHost/NodeID/CertConfig 等基础字段，**完全没有写出** `LimitConfig`、`XrayOptions`、`SingOptions`、`RawOptions`、`Hysteria2ConfigPath`、`ReportMinTraffic` 等关键字段。一旦保存，已有节点的内核相关配置和限速规则就直接被抹掉。
2. **格式与注释丢失**：用户原文件常用 JSON5 写注释、保留字段顺序、用 `Include` 引用外部文件/URL。整文件覆盖会把所有注释、字段顺序、Include 指令展开/丢弃。
3. **未知字段丢失**：项目演进或用户自定义扩展字段不在结构体里时，覆盖写入直接丢弃这些字段，存在数据丢失风险。

用户的脚本化运维场景（自动添加节点、定时同步面板节点列表）依赖"只改我要改的，别动其他东西"的语义，这正是当前实现欠缺的能力。

## What Changes

- **新增**增量写入引擎：基于 JSON 文件的原始字节树（map/array）做最小化修改，只更新调用者明确指定的路径，其他字段、顺序、注释一律保留。
- **重构** `conf.Save()` 为"读取原文件 → 在原始 JSON 树上 patch → 写回"的增量流程；保留 `Save()` 名称但语义从"覆盖"改为"合并"。**BREAKING**（行为级别）：旧的"覆盖整文件"语义不再保留；调用者如果依赖整文件覆盖，需要显式调用新的 `OverwriteSave()`。
- **修复** `NodeConfig.MarshalJSON` 字段遗漏：补全 LimitConfig、XrayOptions、SingOptions、RawOptions、Hysteria2ConfigPath、ReportMinTraffic 等字段；并且只在用户实际设置过的字段上输出，避免向原文件注入大量零值。
- **改造** `cmd/config.go` 中的 `add-node`、`del-node` 走新的增量写入路径，确保对 `Nodes` 数组之外的字段（Log、Cores、其他 Node 的所有自定义字段）零侵入。
- **写入前自动备份**：每次 `Save()` / `OverwriteSave()` 实际写入前，先把原配置文件复制为同目录下的 `<config>.bak`（单文件滚动覆盖，仅保留最近一份）；备份失败时 fail-fast 拒绝写入并保持原文件不变。
- **保留 JSON5 注释**（best-effort）：增量写入过程中读取原始字节并优先以原文回写，仅在结构体改动的子树上做必要重排；当原文件包含纯注释行/块时尽量保留。
- **新增**单元测试覆盖：覆盖 ① 含完整 XrayOptions/SingOptions/LimitConfig 的节点不丢字段；② 含注释的 JSON5 在 add/del 后保留注释；③ 含 Include 引用的节点保留 Include 指令而非展开。

## Capabilities

### New Capabilities
- `config-incremental-write`: 提供"在不丢失原文件其他字段、顺序、注释的前提下，对 V2bX 配置文件做最小化增量修改"的能力，覆盖增删节点、修改单字段等场景。

### Modified Capabilities
<!-- openspec/specs/ 下尚无已归档的 spec（add-config-management 仍处于 changes 中未 archive），因此本变更不对已存在的 specs 做 delta；config-management 的 spec 将在其 archive 时落入 specs/，并可在后续变更里对其做 delta。 -->

## Impact

- Affected specs: `config-incremental-write`（新增 capability）
- Affected code:
  - `conf/conf.go` — 重写 `Save()` 为增量写入，新增内部 raw-json patch 工具
  - `conf/node.go` — 修复 `NodeConfig.MarshalJSON` 字段遗漏
  - `cmd/config.go` — `add-node`/`del-node` 改走增量路径，避免触碰其他节点
  - 新增 `conf/incremental.go`（或 `conf/patch.go`）— 增量写入实现
  - 新增 `conf/backup.go`（或合并入 `conf/incremental.go`）— 写入前原子备份工具
  - 新增对应单元测试 `conf/incremental_test.go`、`conf/node_marshal_test.go`
- Affected dependencies: 不引入新依赖；优先使用标准库 `encoding/json/v2` 的 token/decoder API 在原始字节上定位与替换；如复杂度过高再评估是否引入 `tidwall/sjson` 等。
- Backward compatibility:
  - 配置文件格式不变，不需要用户迁移。
  - 调用 `Save()` 的行为从"完全覆盖"变为"增量合并"，内部调用点（目前仅 `cmd/config.go`）已审计并适配。
  - 对外不暴露的 API 变更不影响用户脚本接口（CLI 命令名与参数保持兼容）。
