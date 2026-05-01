# Project Context

## Purpose
V2bX 是一个基于 Go 语言开发的 V2board 面板节点服务端，修改自 XrayR。
作为代理节点的后端服务，与 V2board 面板 API 对接，负责管理用户代理连接、流量统计、审计规则、TLS 证书等。
支持多种代理内核（Xray / Sing-box / Hysteria2）和多种代理协议（VMess, VLess, Trojan, Shadowsocks, Hysteria1/2, TUIC, AnyTLS）。

## Tech Stack
- Go 1.25（主语言）
- Xray-core（fork 版：wyx2685/xray-core）— V2Ray 代理内核
- Sing-box（fork 版：wyx2685/sing-box_mod）— 多协议代理内核
- Hysteria2（apernet/hysteria）— QUIC 代理内核
- spf13/cobra — CLI 框架
- spf13/viper — 配置管理
- go-resty/resty — HTTP 客户端（与面板 API 通信）
- go-acme/lego — ACME 自动 TLS 证书
- sirupsen/logrus — 结构化日志
- juju/ratelimit — 令牌桶限速
- fsnotify — 文件变更监听（配置热重载）
- encoding/json/v2 + 自研 json5 解析器 — JSON5 配置解析

## Project Conventions

### Code Style
- 遵循 Go 标准代码风格（gofmt）
- 包名使用小写单词
- 公开接口使用大写首字母
- 日志使用 logrus 的 WithField/WithFields 结构化格式
- 错误处理采用 `fmt.Errorf("context: %s", err)` 风格

### Architecture Patterns
- **接口抽象 + 注册机制**：`core.Core` 接口统一三种代理内核，通过 `RegisterCore()` 在各内核 `init()` 中注册工厂函数
- **Selector 模式**：多内核时通过 `Selector` 按协议类型或名称自动分派
- **Controller 模式**：每个面板节点对应一个 `node.Controller`，管理生命周期和定时任务
- **conditions compile（条件编译）**：通过 Go build tags (`-tags "sing xray hysteria2"`) 控制编译哪些内核
- **配置热重载**：通过 fsnotify 监听配置文件变更，自动重启服务

### Testing Strategy
- 单元测试文件与源文件同目录（`_test.go` 后缀）
- 测试数据放在 `test_data/` 目录
- 目前测试覆盖有限，主要集中在配置解析和工具函数

### Git Workflow
- 使用 GitHub 进行版本管理
- CI/CD 配置在 `.github/` 目录下

## Domain Context
- **V2board**：一个代理服务管理面板，本项目是其节点后端
- **节点**：运行代理服务的服务器实例，从面板获取配置和用户列表
- **面板 API**：通过 `/api/v1/server/UniProxy/` 系列端点通信，使用 ETag + SHA256 双重校验避免重复处理
- **Core（内核）**：实际处理代理连接的底层引擎（Xray/Sing-box/Hysteria2）
- **Limiter（限制器）**：控制用户速率、在线 IP 数、TCP 连接数、审计规则
- **定时任务**：PullInterval 拉取面板配置，PushInterval 上报用户流量

## Important Constraints
- 必须搭配 wyx2685 修改版 V2board 面板使用
- 依赖 fork 版本的 sing-box 和 xray-core（通过 go.mod replace 指令）
- 配置文件使用 JSON5 格式（支持注释），默认路径 `/etc/V2bX/config.json`
- Linux 服务管理依赖 systemd
- 构建时需要指定 `GOEXPERIMENT=jsonv2` 环境变量

## External Dependencies
- **V2board 面板 API** — 节点配置、用户列表、流量上报、在线 IP 上报
- **ACME 证书服务** — Let's Encrypt 等，通过 DNS 或 HTTP 验证方式自动申请/续签 TLS 证书
- **DNS 提供商 API** — Cloudflare、阿里云 DNS 等，用于 DNS 验证方式的证书申请
- **GeoIP/GeoSite 数据库** — 用于路由规则判断（geoip.dat, geosite.dat）
