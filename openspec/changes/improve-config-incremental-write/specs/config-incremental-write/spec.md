## ADDED Requirements

### Requirement: 配置文件最小化增量写入
系统 SHALL 提供一种"最小化增量"写入配置文件的能力：在写入时仅修改调用者明确指定的 JSON 路径，原文件中其他字段、数组顺序、对象键顺序与未识别字段均 MUST 保持不变。

#### Scenario: 仅修改 Nodes 字段时不动其他顶层字段
- **WHEN** 调用增量写入接口仅追加一个 `Nodes[*]` 元素
- **THEN** 文件中 `Log`、`Cores`、原有 `Nodes` 元素的所有字段（包括 LimitConfig、XrayOptions、SingOptions、RawOptions、Hysteria2ConfigPath 等）保持原值与原顺序
- **AND** 文件中除新增节点对象外，其他字段的 JSON 字节内容（除空白与必要分隔外）保持稳定

#### Scenario: 包含未知字段的配置文件保持原样
- **WHEN** 原配置文件中存在 V2bX 当前 `Conf` 结构体未声明的字段（如自定义扩展或未来版本字段）
- **THEN** 增量写入操作完成后这些未知字段 MUST 仍出现在输出文件中，且键名、值与位置不变

### Requirement: 节点序列化字段完整性
系统 SHALL 在节点序列化时输出 `NodeConfig.Options` 与 `NodeConfig.ApiConfig` 的全部已设置字段，包括但不限于：`LimitConfig`、`XrayOptions`、`SingOptions`、`RawOptions`、`Hysteria2ConfigPath`、`ReportMinTraffic`、`DeviceOnlineMinTraffic`、`Name`、`CoreName`、`RuleListPath`、`ApiSendIP`。

#### Scenario: 含 XrayOptions 的节点保存后字段不丢失
- **WHEN** 一个 `Core="xray"` 且包含完整 `XrayOptions` 的节点被加载并随后保存
- **THEN** 重新加载该文件得到的 `NodeConfig.Options.XrayOptions` 与原值在结构上等价

#### Scenario: 含 SingOptions 的节点保存后字段不丢失
- **WHEN** 一个 `Core="sing"` 且包含完整 `SingOptions` 的节点被加载并随后保存
- **THEN** 重新加载该文件得到的 `NodeConfig.Options.SingOptions` 与原值在结构上等价

#### Scenario: 含 LimitConfig 的节点保存后限速规则不丢失
- **WHEN** 一个节点包含非零的 `LimitConfig`（速率、连接数、规则等）被加载并随后保存
- **THEN** 重新加载该文件得到的 `NodeConfig.Options.LimitConfig` 与原值字段相等

#### Scenario: 节点零值字段不输出
- **WHEN** 一个节点的某些 Options 字段为 Go 零值（如未设置 `Name`、`LimitConfig` 全零、`Hysteria2ConfigPath=""`）
- **THEN** 序列化输出 MUST 省略这些零值字段，避免向原文件注入冗余空字段

### Requirement: JSON5 注释与格式保留（best-effort）
系统 SHALL 在增量写入时尽力保留原文件中的 JSON5 注释（`//` 行注释、`/* */` 块注释）与字段顺序；当无法在某子树严格保留时，MUST 将无法保留的范围限制在被实际修改的最小子树（例如新增节点对象内部）。

#### Scenario: 顶层注释被保留
- **WHEN** 原文件在 `Nodes` 字段之上有 `// 节点列表` 行注释
- **THEN** 增量追加节点后该注释 MUST 仍存在于原位置

#### Scenario: 被修改子树内的注释允许丢失
- **WHEN** 增量操作整体替换了 `Nodes[2]` 元素
- **THEN** `Nodes[2]` 内部的注释允许丢失，但 `Nodes[0]`、`Nodes[1]`、`Nodes[3..]` 中的注释 MUST 保留

### Requirement: Include 指令保留
系统 SHALL 在增量写入时保留节点对象中的 `Include` 指令字面量，不得在保存时自动展开为内联内容。

#### Scenario: 含 Include 的节点保存后仍是 Include
- **WHEN** 原配置中某节点为 `{"Include": "./nodes/node1.json"}` 形式
- **THEN** 在对其他节点做增删后，该节点 MUST 仍以 `{"Include": "./nodes/node1.json"}` 形式存在于输出文件中
- **AND** Include 引用的外部文件 MUST NOT 被本次写入覆盖

### Requirement: CLI 增删节点走增量路径
系统 SHALL 使 `V2bX config add-node` 与 `V2bX config del-node` 命令在持久化时使用增量写入路径，而非整文件覆盖序列化。

#### Scenario: add-node 不影响其他节点的自定义字段
- **WHEN** 执行 `V2bX config add-node ...` 向一个含两个节点（其中包含完整 XrayOptions/LimitConfig）的配置追加新节点
- **THEN** 命令完成后原有两个节点的 XrayOptions 与 LimitConfig 字段 MUST 与执行前完全相同
- **AND** 新节点 MUST 出现在 `Nodes` 数组末尾

#### Scenario: del-node 不影响保留节点的自定义字段
- **WHEN** 执行 `V2bX config del-node 2` 删除三节点配置中的第二个节点
- **THEN** 剩余两个节点的所有字段（包括 XrayOptions/SingOptions/LimitConfig/Include）MUST 保持不变
- **AND** `Nodes` 数组长度从 3 变为 2

### Requirement: 整文件覆盖能力作为显式选项保留
系统 SHALL 保留一种显式的整文件覆盖写入入口（例如 `OverwriteSave`），供未来需要"格式化重写整文件"场景使用，但默认 `Save()` 行为 MUST 为增量写入。

#### Scenario: 默认 Save 为增量
- **WHEN** 调用 `Conf.Save(path)` 而原文件存在
- **THEN** 写入行为 MUST 走增量路径

#### Scenario: 显式覆盖写入
- **WHEN** 调用 `Conf.OverwriteSave(path)`（或等价命名）
- **THEN** 写入行为 MUST 完全替换文件内容，不保留原文件的注释与字段顺序

### Requirement: 增量写入失败时不破坏原文件
系统 SHALL 在增量写入过程中保证原子性：当任意中间步骤失败（解析失败、磁盘写入失败、权限错误等）时，原配置文件 MUST 保持失败前的内容不变。

#### Scenario: 写入磁盘失败保留原文件
- **WHEN** 增量写入中途因磁盘空间不足失败
- **THEN** 原 `config.json` MUST 仍是失败前的字节内容
- **AND** 命令 MUST 返回明确错误信息

#### Scenario: 解析原文件失败时拒绝写入
- **WHEN** 原文件存在但 JSON5 语法不合法
- **THEN** 增量写入 MUST 直接返回解析错误并退出，不得清空或写入半成品文件

### Requirement: 写入前自动备份原配置
系统 SHALL 在 `Save()` 与 `OverwriteSave()` 实际写入前，将原配置文件复制为同目录下的 `<配置文件名>.bak`，作为"上一次成功状态"的单文件滚动备份。

#### Scenario: 首次写入自动产生备份
- **WHEN** 原配置文件存在但 `<path>.bak` 不存在
- **THEN** 写入前 MUST 创建 `<path>.bak`
- **AND** `.bak` 内容与写入前的原文件字节完全一致

#### Scenario: 后续写入滚动覆盖备份
- **WHEN** 原配置文件存在且 `<path>.bak` 已存在
- **THEN** 写入前 MUST 用当前原文件覆盖 `.bak`
- **AND** 系统 MUST NOT 保留更早版本（单文件滚动）

#### Scenario: 原文件不存在时不创建空备份
- **WHEN** 原配置文件不存在（首次部署写入新文件）
- **THEN** MUST NOT 创建任何 `.bak` 文件
- **AND** 写入路径退化为 `OverwriteSave` 的新建行为

#### Scenario: 备份失败拒绝写入
- **WHEN** 复制原文件到 `.bak` 因权限或磁盘错误失败
- **THEN** 写入操作 MUST 返回明确错误并退出
- **AND** 原配置文件 MUST 保持不变
- **AND** MUST NOT 继续创建 `<path>.tmp` 或写入新内容

#### Scenario: 备份本身原子化
- **WHEN** 在写 `.bak` 过程中进程被中断
- **THEN** 文件系统上 MUST NOT 出现内容残缺的 `.bak` 文件
- **AND** 实现 MUST 通过先写 `.bak.tmp` 再 `rename` 的方式保证原子性
