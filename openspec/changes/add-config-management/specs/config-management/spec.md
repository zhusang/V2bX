## ADDED Requirements

### Requirement: 配置文件保存
系统 SHALL 提供将当前内存中的配置结构序列化为 JSON 格式并写回文件的能力。

#### Scenario: 保存配置到文件
- **WHEN** 调用 Save 方法并传入目标文件路径
- **THEN** 配置以格式化 JSON（2空格缩进）写入该文件
- **AND** 写入的文件可被 LoadFromPath 正常加载

#### Scenario: 保存到不存在的路径
- **WHEN** 目标文件所在目录不存在
- **THEN** 返回明确错误信息

### Requirement: 查看当前配置
系统 SHALL 提供 `V2bX config show` 命令，输出当前配置文件的完整内容。

#### Scenario: 正常查看
- **WHEN** 用户执行 `V2bX config show -c /path/to/config.json`
- **THEN** 以格式化 JSON 输出完整配置内容到标准输出

#### Scenario: 配置文件不存在
- **WHEN** 指定的配置文件路径不存在
- **THEN** 输出明确错误提示

### Requirement: 列出所有节点
系统 SHALL 提供 `V2bX config list-nodes` 命令，以表格形式展示已配置的节点列表。

#### Scenario: 正常列出
- **WHEN** 用户执行 `V2bX config list-nodes`
- **THEN** 输出包含序号（从1开始）、APIHost、NodeType、NodeID、Core 等字段的表格

#### Scenario: 无节点配置
- **WHEN** 配置中没有节点
- **THEN** 输出提示信息 "没有配置任何节点"

### Requirement: 添加节点
系统 SHALL 提供 `V2bX config add-node` 命令，通过命令行参数添加一个新节点到现有配置中。

#### Scenario: 通过参数添加节点
- **WHEN** 用户执行 `V2bX config add-node --api-host http://panel.example.com --api-key mykey --node-id 1 --node-type vmess`
- **THEN** 将新节点追加到配置的 Nodes 数组中
- **AND** 自动保存配置文件

#### Scenario: 缺少必需参数
- **WHEN** 用户执行 `V2bX config add-node` 但未提供 `--api-host` 或 `--node-id` 等必需参数
- **THEN** 输出错误提示，告知缺少哪些参数

### Requirement: 删除节点
系统 SHALL 提供 `V2bX config del-node` 命令，按索引删除指定节点。

#### Scenario: 按索引删除节点
- **WHEN** 用户执行 `V2bX config del-node 1`
- **THEN** 删除配置中索引为 1 的节点（从1开始计数）
- **AND** 自动保存配置文件

#### Scenario: 索引越界
- **WHEN** 用户执行 `V2bX config del-node 99` 但配置中只有 2 个节点
- **THEN** 输出错误提示 "节点索引超出范围"
