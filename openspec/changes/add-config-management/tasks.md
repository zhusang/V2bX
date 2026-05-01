## 1. 基础设施
- [ ] 1.1 在 `conf` 包新增 `Save(filePath string) error` 方法，将 `Conf` 结构序列化为格式化 JSON 并写入文件
- [ ] 1.2 确保 Save 方法保留 JSON 缩进格式（2空格），输出可读性好的配置文件

## 2. CLI 命令 — 查看
- [ ] 2.1 新建 `cmd/config.go`，定义 `config` 父命令
- [ ] 2.2 实现 `config show` 子命令：加载配置文件 → 格式化输出完整 JSON
- [ ] 2.3 实现 `config list-nodes` 子命令：以表格形式展示所有节点（序号、APIHost、NodeType、NodeID、Core）

## 3. CLI 命令 — 增删节点
- [ ] 3.1 实现 `config add-node` 子命令：通过 `--api-host`、`--api-key`、`--node-id`、`--node-type`、`--core` 等参数添加节点
- [ ] 3.2 为 add-node 设置合理的默认值（ListenIP=0.0.0.0, SendIP=0.0.0.0, Timeout=30 等）
- [ ] 3.3 实现 `config del-node` 子命令：通过 `--index` 参数或位置参数指定要删除的节点索引
- [ ] 3.4 删除前进行索引边界检查和确认提示

## 4. 测试与验证
- [ ] 4.1 对 Save 方法编写单元测试（写入后重新读取比对）
- [ ] 4.2 手动测试 CLI 命令的增删节点流程
