## Context

V2bX 配置 `/etc/V2bX/config.json` 在线上场景常常需要由脚本自动维护：节点上下线、Provisioning 流水线追加节点、限速规则批量更新等。当前实现存在三处现状：

1. `conf/conf.go` 的 `Save()` 实现为：
   ```go
   data, _ := stdjson.MarshalIndent(p, "", "  ")
   os.WriteFile(filePath, data, 0644)
   ```
   即以"先把 Conf 整体序列化，再覆盖整个文件"的方式工作。
2. `conf/node.go` 的 `NodeConfig.MarshalJSON` 显式 map 化输出，但只覆盖了 ApiConfig 的核心字段与少数 Options 字段，**遗漏了 LimitConfig、RawOptions、XrayOptions、SingOptions、Hysteria2ConfigPath、ReportMinTraffic 等重要字段**。
3. `cmd/config.go` 的 `add-node`/`del-node` 在内存中修改 `c.NodeConfig` 后调用上述 `Save()`，因此一次"增加节点"会重写整文件，并丢失被 `MarshalJSON` 跳过的字段。

利益相关方：

- 运维 / 脚本作者：希望"增加一个节点"不要影响其他节点的限速、Xray/Sing 选项、自定义扩展字段。
- V2bX 维护者：希望保持配置文件可读性（注释、字段顺序）以便排错。
- 升级路径：当前使用 `Save()` 的代码点只有 `cmd/config.go`，因此修改成本可控。

约束：

- Go 1.25，已使用 `encoding/json/v2`（带 `GOEXPERIMENT=jsonv2`）与自研 `json5` 解析器。
- 不希望引入重型 JSON tree 操作依赖，优先用标准库与现有 `json5` 工具。
- 配置文件可能很大（含多节点、内嵌完整 Xray 配置），应避免显著的额外内存占用与拷贝。

## Goals / Non-Goals

**Goals:**

- 提供"读 → patch 原始 JSON 树 → 写"的最小化增量写入引擎。
- 修复 `NodeConfig.MarshalJSON` 字段遗漏，使节点序列化结构完整。
- 让 CLI `add-node`/`del-node` 走增量路径，不影响其他节点的字段、不破坏 Include。
- 保留原文件的字段顺序、未知字段；尽力保留 JSON5 注释。
- 写入过程对原文件保持原子性。

**Non-Goals:**

- 不重新设计 V2bX 的配置 schema 或迁移到其他配置格式（YAML/TOML）。
- 不支持 deep-merge 用户提交的"局部 JSON patch"作为公开命令（保持 CLI 接口稳定，仅 add-node/del-node 走增量）。
- 不强制保留所有 JSON5 注释（best-effort）：被替换的最小子树内的注释允许丢失。
- 不修改 `LoadFromPath` 行为；加载侧仍走结构体反序列化。
- 不引入新的 CLI 命令（如 `config set-field`）；保留为后续变更的扩展点。

## Decisions

### 决策 1：增量写入的实现策略 — "原始 JSON 树 patch + 原始字节切片回写"

选择方案：用 `encoding/json/v2` 把原文件解析成 `*ordered.Map` / `[]any` 这种保留键顺序的中间结构，针对 `Nodes` 数组做最小修改（追加/删除/替换某下标），再用 `MarshalIndent` 写出。

**为什么不直接用 `map[string]any`**：标准 `map` 不保留键顺序，会破坏文件结构。

**为什么不引入 `tidwall/sjson`**：sjson 是不错的选择，但它的语义偏向"路径式 set/delete"，对数组 reorder/插入下标 N 的支持需要包装；且会引入新的依赖。我们的写入路径目前只有"追加节点 / 按下标删除节点"两类，自研一个小工具足够，后续若新增更多 patch 场景再评估替换。

**实现要点**：
- 用 `encoding/json/v2` 的 `jsontext.Decoder` 按 token 流式扫描原文件，记录每个顶层 key（Log/Cores/Nodes/...）在原始字节中的 `[start, end)` 区间。
- 对未被本次操作影响的字段：直接复制 `[start, end)` 原始字节到输出，从而保留顺序、注释（紧邻字段的注释会被一并 copy）、未知字段。
- 对被影响的字段（仅 `Nodes`）：用结构体重新序列化整个数组，但每个节点对象内部使用更新后的 `MarshalJSON`。
- 原文件中无 `Nodes` 字段时，将其追加在 `Cores` 之后或文件末尾。

**注释保留范围**：行注释/块注释如果出现在两个顶层字段之间，会被自动保留（因为我们 copy 原始字节）；出现在被替换数组内部的，按 best-effort 丢失。

### 决策 2：原子写入用 "写临时文件 + rename"

实现：
```
tmp := filePath + ".tmp"
os.WriteFile(tmp, newBytes, 0644)
os.Rename(tmp, filePath)
```

**为什么**：`os.WriteFile` 会先 truncate 再写，写到一半失败则原文件已损坏。临时文件 + rename 在 POSIX 下是原子操作；Windows 上 rename 不是严格原子但优于直接 truncate，并且我们的目标平台主要是 Linux。

**临时文件位置**：放在原文件相同目录，避免跨设备 rename 失败。

### 决策 3：`NodeConfig.MarshalJSON` 重写为"反射 / 显式映射所有字段"

选择方案：保留显式 map 写法（避免 `omitempty` 在 `encoding/json/v2` 下与自定义类型交互的边界情况），但补全所有字段，并对每个字段做"是否零值"判断决定是否输出。零值判断对 `LimitConfig`、`*XrayOptions`、`*SingOptions` 等使用 `reflect.DeepEqual` 与对应零值比较。

**为什么不直接 `type alias` 走默认序列化**：`Options` 包含 `RawOptions json.RawMessage` 字段，默认序列化会同时输出 `RawOptions` 与 `XrayOptions/SingOptions`，造成键冲突或冗余。显式 map 更可控。

**字段清单**（必须全部覆盖）：
- ApiConfig: APIHost, APISendIP, NodeID, Key (ApiKey), NodeType, Timeout, RuleListPath
- Options: Name, Core, CoreName, ListenIP, SendIP, DeviceOnlineMinTraffic, ReportMinTraffic, LimitConfig, RawOptions, XrayOptions, SingOptions, Hysteria2ConfigPath, CertConfig

### 决策 4：保留 `Save()` 名称，新增 `OverwriteSave()` 作为旧行为出口

**为什么**：`Save()` 是公开 API，现有外部使用者（虽然项目内目前只有 cmd/config.go）期望"持久化当前配置"，将其语义升级为"增量持久化"对调用者更友好且更安全。`OverwriteSave()` 作为显式覆盖出口，供未来"用户主动重置整文件格式"等场景使用。

### 决策 5：CLI 命令的 UX 不变

`V2bX config add-node` / `del-node` 的参数与输出文案保持兼容；只有"持久化阶段的字节级行为"改变。这样所有现有运维脚本不需要修改。

### 决策 6：写入前单文件滚动备份

实现：每次 `Save()` / `OverwriteSave()` 在写入前，把原文件复制为同目录下的 `<filePath>.bak`，覆盖式滚动（始终只保留最近一份）。复制本身原子化：先写 `<filePath>.bak.tmp`，`fsync` 后 `rename` 为 `<filePath>.bak`。

**为什么不要时间戳多版本**：滚动方案零清理成本、文件名稳定、可覆盖"误操作回退一步"的 90% 场景。多版本备份 + `V2bX config restore` CLI 留给后续单独变更评估，避免本次变更范围爆炸。

**为什么备份失败要 fail-fast**：备份是写入前的安全网，失败往往意味着磁盘/权限异常。此时即使继续写入，原子写入流程也容易二次受损且失去回退基准；与 Requirement 7"写入失败不破坏原文件"保持一致的严格语义更符合用户预期。

**为什么放在写入流水线最前置步骤**：执行顺序是 ① 备份 → ② 解析原文件 → ③ patch 内存 → ④ 写 `<path>.tmp` → ⑤ rename 到 `<path>`。这样任何中途失败都能从 `.bak` 手工回退；原文件不存在的首次部署场景则直接跳过 ① 进入 `OverwriteSave` 新建路径。

## Risks / Trade-offs

- [JSON5 注释保留依赖原始字节 copy] → 若原文件包含极端语法（嵌套到 `Nodes` 内部的多层 include 引用、超大单行 JSON），字节区间定位可能复杂 → **缓解**：先用 `encoding/json/v2` 的 `jsontext.Decoder` 取得每个 token 的字节偏移；增加针对超大节点（>1MB）与多层嵌套的回归测试。
- [`encoding/json/v2` 仍是实验特性] → API 表面变化可能导致升级摩擦 → **缓解**：把 patch 工具写成内部包，集中所有 `jsonv2` 调用，必要时未来切换到稳定 API 只需改一处。
- [Windows 下 rename 非严格原子] → 极端断电场景仍可能损坏文件 → **缓解**：先 fsync 临时文件，再 rename；文档化"主要在 Linux 上验证原子性"。
- [新 MarshalJSON 输出可能与既有节点的 LoadFromPath 不对称] → 比如 RawOptions 与 XrayOptions 同时存在的边界 → **缓解**：单测断言 `LoadFromPath(Save(c)) == c`（结构等价）；对 `Core="xray"` / `"sing"` / `"hysteria2"` / 空 四类核心都覆盖。
- [增量写入引擎复杂度上升导致 bug 风险] → **缓解**：限定本次仅支持"追加 Nodes、删除 Nodes 下标"两个 patch 操作；其他需要走增量的场景在新变更里追加。

## Migration Plan

1. **代码侧**：`Save()` 行为变更，但调用方仅 `cmd/config.go`，已在本变更里同步适配。无对外 API breaking。
2. **配置文件侧**：用户原 `config.json` 不需要任何修改；首次升级后调用 `add-node` 即可获得增量行为。
3. **回滚**：若发现严重 bug，回滚 commit 即可恢复"覆盖式 Save"。期间用户已经写入的配置仍能被旧版 V2bX 正常加载（输出格式仍是合法 JSON5）。
4. **验证**：在 `test_data/` 增加一个含 XrayOptions/SingOptions/LimitConfig/注释/Include 的配置样本，单测覆盖 add/del 路径后比对原字段不变。

## Open Questions

- 是否需要在 `Save()` 增量路径里支持"原文件不存在"的场景？倾向于：不存在时退化为 `OverwriteSave()` 写新文件，但需要在 spec 里写清楚。
- `OverwriteSave()` 的命名是否合适？候选：`SaveAll`、`Rewrite`、`SaveOverwrite`。倾向 `OverwriteSave` 因语义直白，最终在实现 PR 里复核。
- 当用户原文件中 `Nodes` 字段使用 `Include` 引用了一个外部数组文件时，本次"增量增删 Nodes"应该改外部文件还是改主文件？**初步立场**：仅改主文件中显式列出的部分；若 `Nodes` 字段整体是 `Include`，本次不支持增量，应返回明确错误并提示用户。需要在 spec 后续迭代时再细化。
