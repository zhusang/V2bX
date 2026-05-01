package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	stdjson "encoding/json"

	"github.com/InazumaV/V2bX/conf"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var configCommand = cobra.Command{
	Use:   "config",
	Short: "Manage V2bX configuration",
}

var configShowCommand = cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Run:   configShowHandle,
}

var configListNodesCommand = cobra.Command{
	Use:   "list-nodes",
	Short: "List all configured nodes",
	Run:   configListNodesHandle,
}

var (
	addNodeApiHost  string
	addNodeApiKey   string
	addNodeNodeID   int
	addNodeNodeType string
	addNodeCore     string
	addNodeListenIP string
	addNodeSendIP   string
	addNodeTimeout  int
)

var configAddNodeCommand = cobra.Command{
	Use:   "add-node",
	Short: "Add a new node to configuration",
	Run:   configAddNodeHandle,
}

var configDelNodeCommand = cobra.Command{
	Use:   "del-node <index>",
	Short: "Delete a node by index (starting from 1)",
	Args:  cobra.ExactArgs(1),
	Run:   configDelNodeHandle,
}

func init() {
	configCommand.PersistentFlags().
		StringVarP(&config, "config", "c",
			"/etc/V2bX/config.json", "config file path")

	configAddNodeCommand.Flags().StringVar(&addNodeApiHost, "api-host", "", "Panel API host (required)")
	configAddNodeCommand.Flags().StringVar(&addNodeApiKey, "api-key", "", "Panel API key (required)")
	configAddNodeCommand.Flags().IntVar(&addNodeNodeID, "node-id", 0, "Node ID (required)")
	configAddNodeCommand.Flags().StringVar(&addNodeNodeType, "node-type", "", "Node type: vmess,vless,trojan,shadowsocks,hysteria,hysteria2 (required)")
	configAddNodeCommand.Flags().StringVar(&addNodeCore, "core", "", "Core to use: xray,sing,hysteria2")
	configAddNodeCommand.Flags().StringVar(&addNodeListenIP, "listen-ip", "0.0.0.0", "Listen IP")
	configAddNodeCommand.Flags().StringVar(&addNodeSendIP, "send-ip", "0.0.0.0", "Send IP")
	configAddNodeCommand.Flags().IntVar(&addNodeTimeout, "timeout", 30, "API timeout in seconds")

	configCommand.AddCommand(&configShowCommand)
	configCommand.AddCommand(&configListNodesCommand)
	configCommand.AddCommand(&configAddNodeCommand)
	configCommand.AddCommand(&configDelNodeCommand)

	command.AddCommand(&configCommand)
}

func loadConfig() (*conf.Conf, error) {
	c := conf.New()
	err := c.LoadFromPath(config)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func configShowHandle(_ *cobra.Command, _ []string) {
	c, err := loadConfig()
	if err != nil {
		log.WithField("err", err).Error("Load config file failed")
		return
	}
	data, err := stdjson.MarshalIndent(c, "", "  ")
	if err != nil {
		log.WithField("err", err).Error("Marshal config failed")
		return
	}
	fmt.Println(string(data))
}

func configListNodesHandle(_ *cobra.Command, _ []string) {
	c, err := loadConfig()
	if err != nil {
		log.WithField("err", err).Error("Load config file failed")
		return
	}
	nodes := c.NodeConfig
	if len(nodes) == 0 {
		fmt.Println("没有配置任何节点")
		return
	}

	// Print table header
	fmt.Printf("%-6s %-30s %-15s %-8s %-10s\n",
		"Index", "APIHost", "NodeType", "NodeID", "Core")
	fmt.Println(strings.Repeat("-", 75))

	for i, n := range nodes {
		core := n.Options.Core
		if core == "" {
			core = "-"
		}
		fmt.Printf("%-6d %-30s %-15s %-8d %-10s\n",
			i+1,
			n.ApiConfig.APIHost,
			n.ApiConfig.NodeType,
			n.ApiConfig.NodeID,
			core,
		)
	}
}

func configAddNodeHandle(_ *cobra.Command, _ []string) {
	// Validate required params
	var missing []string
	if addNodeApiHost == "" {
		missing = append(missing, "--api-host")
	}
	if addNodeApiKey == "" {
		missing = append(missing, "--api-key")
	}
	if addNodeNodeID == 0 {
		missing = append(missing, "--node-id")
	}
	if addNodeNodeType == "" {
		missing = append(missing, "--node-type")
	}
	if len(missing) > 0 {
		fmt.Println(Err("缺少必需参数: ", strings.Join(missing, ", ")))
		os.Exit(2)
	}

	c, err := loadConfig()
	if err != nil {
		log.WithField("err", err).Error("Load config file failed")
		os.Exit(1)
	}

	newNode := conf.NodeConfig{
		ApiConfig: conf.ApiConfig{
			APIHost:  addNodeApiHost,
			Key:      addNodeApiKey,
			NodeID:   addNodeNodeID,
			NodeType: addNodeNodeType,
			Timeout:  addNodeTimeout,
		},
		Options: conf.Options{
			Core:       addNodeCore,
			ListenIP:   addNodeListenIP,
			SendIP:     addNodeSendIP,
			CertConfig: conf.NewCertConfig(),
		},
	}

	c.NodeConfig = append(c.NodeConfig, newNode)

	err = c.Save(config)
	if err != nil {
		log.WithField("err", err).Error("Save config file failed")
		fmt.Println(Err("保存配置文件失败: ", err.Error()))
		os.Exit(1)
	}

	fmt.Println(Ok(fmt.Sprintf("节点已添加成功 (NodeID: %d, Type: %s)", addNodeNodeID, addNodeNodeType)))
}

func configDelNodeHandle(_ *cobra.Command, args []string) {
	index, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Println(Err("无效的索引值: ", args[0]))
		os.Exit(2)
	}

	c, err := loadConfig()
	if err != nil {
		log.WithField("err", err).Error("Load config file failed")
		os.Exit(1)
	}

	if index < 1 || index > len(c.NodeConfig) {
		fmt.Println(Err(fmt.Sprintf("节点索引超出范围 (有效范围: 1-%d)", len(c.NodeConfig))))
		os.Exit(2)
	}

	deleted := c.NodeConfig[index-1]
	c.NodeConfig = append(c.NodeConfig[:index-1], c.NodeConfig[index:]...)

	err = c.Save(config)
	if err != nil {
		log.WithField("err", err).Error("Save config file failed")
		fmt.Println(Err("保存配置文件失败: ", err.Error()))
		os.Exit(1)
	}

	fmt.Println(Ok(fmt.Sprintf("节点已删除 (NodeID: %d, Type: %s, Host: %s)",
		deleted.ApiConfig.NodeID,
		deleted.ApiConfig.NodeType,
		deleted.ApiConfig.APIHost,
	)))
}

// configFlag returns the config file path, checking if it was explicitly set
func init() {
	// Ensure config flag is available before any subcommand runs
	// The flag is already registered via configCommand.PersistentFlags above
}
