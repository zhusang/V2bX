package conf

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"encoding/json"

	"github.com/InazumaV/V2bX/common/json5"
)

type NodeConfig struct {
	ApiConfig ApiConfig `json:"-"`
	Options   Options   `json:"-"`
	// OriginalInclude preserves the original `Include` directive so that
	// re-serializing the node emits `{"Include": "..."}` rather than the
	// inlined content. Empty when the node was inlined directly.
	OriginalInclude string `json:"-"`
}

func (n NodeConfig) MarshalJSON() ([]byte, error) {
	// If this node was loaded via an Include directive, keep that directive
	// on output so external file references are not silently inlined.
	if n.OriginalInclude != "" {
		return json.Marshal(map[string]string{"Include": n.OriginalInclude})
	}

	m := make(map[string]interface{})

	// ApiConfig fields (always emit core ones; optional ones only when set)
	m["ApiHost"] = n.ApiConfig.APIHost
	m["ApiKey"] = n.ApiConfig.Key
	m["NodeID"] = n.ApiConfig.NodeID
	m["NodeType"] = n.ApiConfig.NodeType
	m["Timeout"] = n.ApiConfig.Timeout
	if n.ApiConfig.APISendIP != "" {
		m["ApiSendIP"] = n.ApiConfig.APISendIP
	}
	if n.ApiConfig.RuleListPath != "" {
		m["RuleListPath"] = n.ApiConfig.RuleListPath
	}

	// Options common fields
	if n.Options.Name != "" {
		m["Name"] = n.Options.Name
	}
	if n.Options.Core != "" {
		m["Core"] = n.Options.Core
	}
	if n.Options.CoreName != "" {
		m["CoreName"] = n.Options.CoreName
	}
	m["ListenIP"] = n.Options.ListenIP
	m["SendIP"] = n.Options.SendIP
	if n.Options.DeviceOnlineMinTraffic != 0 {
		m["DeviceOnlineMinTraffic"] = n.Options.DeviceOnlineMinTraffic
	}
	if n.Options.ReportMinTraffic != 0 {
		m["ReportMinTraffic"] = n.Options.ReportMinTraffic
	}
	if !isZeroLimitConfig(n.Options.LimitConfig) {
		m["LimitConfig"] = n.Options.LimitConfig
	}
	if n.Options.Hysteria2ConfigPath != "" {
		m["Hysteria2ConfigPath"] = n.Options.Hysteria2ConfigPath
	}
	if n.Options.CertConfig != nil {
		m["CertConfig"] = n.Options.CertConfig
	}

	// Core-specific structured options: flatten into the top-level object
	// to match the round-trip expected by Options.UnmarshalJSON, which
	// unmarshals the flat node body into XrayOptions / SingOptions directly.
	switch n.Options.Core {
	case "xray":
		if n.Options.XrayOptions != nil {
			if err := mergeStructIntoMap(m, n.Options.XrayOptions); err != nil {
				return nil, err
			}
		}
	case "sing":
		if n.Options.SingOptions != nil {
			if err := mergeStructIntoMap(m, n.Options.SingOptions); err != nil {
				return nil, err
			}
		}
	}

	// RawOptions: for hysteria2 / unknown cores the original full body was
	// captured into RawOptions; merge any extra unknown keys without
	// overriding fields we already populated explicitly.
	if (n.Options.Core == "hysteria2" || n.Options.Core == "") && len(n.Options.RawOptions) > 0 {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(n.Options.RawOptions, &rawMap); err == nil {
			for k, v := range rawMap {
				if _, exists := m[k]; !exists {
					m[k] = v
				}
			}
		}
	}

	return json.Marshal(m)
}

// mergeStructIntoMap marshals v then merges its top-level keys into m.
// Values from v take precedence over existing keys in m (caller controls
// the precedence by ordering the calls).
func mergeStructIntoMap(m map[string]interface{}, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var sub map[string]interface{}
	if err := json.Unmarshal(data, &sub); err != nil {
		return err
	}
	for k, val := range sub {
		m[k] = val
	}
	return nil
}

func isZeroLimitConfig(c LimitConfig) bool {
	return !c.EnableRealtime && c.SpeedLimit == 0 && c.IPLimit == 0 && c.ConnLimit == 0 &&
		!c.EnableIpRecorder && c.IpRecorderConfig == nil &&
		!c.EnableDynamicSpeedLimit && c.DynamicSpeedLimitConfig == nil
}

type rawNodeConfig struct {
	Include string          `json:"Include"`
	ApiRaw  json.RawMessage `json:"ApiConfig"`
	OptRaw  json.RawMessage `json:"Options"`
}

type ApiConfig struct {
	APIHost      string `json:"ApiHost"`
	APISendIP    string `json:"ApiSendIP"`
	NodeID       int    `json:"NodeID"`
	Key          string `json:"ApiKey"`
	NodeType     string `json:"NodeType"`
	Timeout      int    `json:"Timeout"`
	RuleListPath string `json:"RuleListPath"`
}

func (n *NodeConfig) UnmarshalJSON(data []byte) (err error) {
	rn := rawNodeConfig{}
	err = json.Unmarshal(data, &rn)
	if err != nil {
		return err
	}
	if len(rn.Include) != 0 {
		n.OriginalInclude = rn.Include
		file, _ := strings.CutPrefix(rn.Include, ":")
		switch file {
		case "http", "https":
			rsp, err := http.Get(file)
			if err != nil {
				return err
			}
			defer rsp.Body.Close()
			data, err = io.ReadAll(json5.NewTrimNodeReader(rsp.Body))
			if err != nil {
				return fmt.Errorf("open include file error: %s", err)
			}
		default:
			f, err := os.Open(rn.Include)
			if err != nil {
				return fmt.Errorf("open include file error: %s", err)
			}
			defer f.Close()
			data, err = io.ReadAll(json5.NewTrimNodeReader(f))
			if err != nil {
				return fmt.Errorf("open include file error: %s", err)
			}
		}
		err = json.Unmarshal(data, &rn)
		if err != nil {
			return fmt.Errorf("unmarshal include file error: %s", err)
		}
	}

	n.ApiConfig = ApiConfig{
		APIHost: "http://127.0.0.1",
		Timeout: 30,
	}
	if len(rn.ApiRaw) > 0 {
		err = json.Unmarshal(rn.ApiRaw, &n.ApiConfig)
		if err != nil {
			return
		}
	} else {
		err = json.Unmarshal(data, &n.ApiConfig)
		if err != nil {
			return
		}
	}

	n.Options = Options{
		ListenIP:   "0.0.0.0",
		SendIP:     "0.0.0.0",
		CertConfig: NewCertConfig(),
	}
	if len(rn.OptRaw) > 0 {
		err = json.Unmarshal(rn.OptRaw, &n.Options)
		if err != nil {
			return
		}
	} else {
		err = json.Unmarshal(data, &n.Options)
		if err != nil {
			return
		}
	}
	return
}

type Options struct {
	Name                   string          `json:"Name"`
	Core                   string          `json:"Core"`
	CoreName               string          `json:"CoreName"`
	ListenIP               string          `json:"ListenIP"`
	SendIP                 string          `json:"SendIP"`
	DeviceOnlineMinTraffic int64           `json:"DeviceOnlineMinTraffic"`
	ReportMinTraffic       int64           `json:"ReportMinTraffic"`
	LimitConfig            LimitConfig     `json:"LimitConfig"`
	RawOptions             json.RawMessage `json:"RawOptions"`
	XrayOptions            *XrayOptions    `json:"XrayOptions"`
	SingOptions            *SingOptions    `json:"SingOptions"`
	Hysteria2ConfigPath    string          `json:"Hysteria2ConfigPath"`
	CertConfig             *CertConfig     `json:"CertConfig"`
}

func (o *Options) UnmarshalJSON(data []byte) error {
	type opt Options
	err := json.Unmarshal(data, (*opt)(o))
	if err != nil {
		return err
	}
	switch o.Core {
	case "xray":
		o.XrayOptions = NewXrayOptions()
		return json.Unmarshal(data, o.XrayOptions)
	case "sing":
		o.SingOptions = NewSingOptions()
		return json.Unmarshal(data, o.SingOptions)
	case "hysteria2":
		o.RawOptions = data
		return nil
	default:
		o.Core = ""
		o.RawOptions = data
	}
	return nil
}
