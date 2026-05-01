package conf

import (
	stdjson "encoding/json"
	"reflect"
	"strings"
	"testing"
)

// roundTrip marshals the given NodeConfig to JSON, then unmarshals back into
// a new NodeConfig and returns it. This simulates the Save→Load cycle and is
// the canonical test for "no fields are dropped on persistence".
func roundTrip(t *testing.T, n NodeConfig) NodeConfig {
	t.Helper()
	data, err := stdjson.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out NodeConfig
	if err := stdjson.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	return out
}

func TestNodeMarshal_XrayCore_RoundTrip(t *testing.T) {
	n := NodeConfig{
		ApiConfig: ApiConfig{
			APIHost:  "http://panel.example.com",
			Key:      "secret",
			NodeID:   42,
			NodeType: "vmess",
			Timeout:  30,
		},
		Options: Options{
			Core:     "xray",
			ListenIP: "0.0.0.0",
			SendIP:   "0.0.0.0",
			LimitConfig: LimitConfig{
				EnableRealtime: true,
				SpeedLimit:     1000,
				IPLimit:        3,
			},
			XrayOptions: &XrayOptions{
				EnableProxyProtocol: true,
				EnableTFO:           true,
				DNSType:             "AsIs",
			},
			CertConfig: NewCertConfig(),
		},
	}
	got := roundTrip(t, n)

	if got.ApiConfig != n.ApiConfig {
		t.Errorf("ApiConfig mismatch: got %+v want %+v", got.ApiConfig, n.ApiConfig)
	}
	if got.Options.Core != "xray" {
		t.Errorf("Core lost: got %q", got.Options.Core)
	}
	if got.Options.XrayOptions == nil {
		t.Fatalf("XrayOptions dropped on round-trip")
	}
	if !got.Options.XrayOptions.EnableProxyProtocol || !got.Options.XrayOptions.EnableTFO {
		t.Errorf("XrayOptions fields lost: %+v", got.Options.XrayOptions)
	}
	if got.Options.LimitConfig.SpeedLimit != 1000 || got.Options.LimitConfig.IPLimit != 3 {
		t.Errorf("LimitConfig lost: %+v", got.Options.LimitConfig)
	}
}

func TestNodeMarshal_SingCore_RoundTrip(t *testing.T) {
	n := NodeConfig{
		ApiConfig: ApiConfig{
			APIHost:  "http://panel.example.com",
			Key:      "k",
			NodeID:   1,
			NodeType: "shadowsocks",
			Timeout:  30,
		},
		Options: Options{
			Core:     "sing",
			ListenIP: "0.0.0.0",
			SendIP:   "0.0.0.0",
			SingOptions: &SingOptions{
				TCPFastOpen:              true,
				SniffEnabled:             true,
				SniffOverrideDestination: true,
			},
			CertConfig: NewCertConfig(),
		},
	}
	got := roundTrip(t, n)
	if got.Options.Core != "sing" {
		t.Errorf("Core lost: got %q", got.Options.Core)
	}
	if got.Options.SingOptions == nil {
		t.Fatalf("SingOptions dropped on round-trip")
	}
	if !got.Options.SingOptions.TCPFastOpen {
		t.Errorf("TCPFastOpen lost")
	}
}

func TestNodeMarshal_Hysteria2Core_RoundTrip(t *testing.T) {
	// Hysteria2 keeps RawOptions verbatim. The original payload includes
	// arbitrary keys; we ensure nothing essential is dropped.
	src := []byte(`{
		"ApiHost":   "http://panel",
		"ApiKey":    "k",
		"NodeID":    7,
		"NodeType":  "hysteria2",
		"Timeout":   30,
		"Core":      "hysteria2",
		"ListenIP":  "0.0.0.0",
		"SendIP":    "0.0.0.0",
		"Hysteria2ConfigPath": "/etc/V2bX/hy2.json",
		"customExt": "preserve-me"
	}`)
	var n NodeConfig
	if err := stdjson.Unmarshal(src, &n); err != nil {
		t.Fatalf("unmarshal source: %v", err)
	}
	got := roundTrip(t, n)
	if got.Options.Core != "hysteria2" {
		t.Errorf("Core lost: %q", got.Options.Core)
	}
	if got.Options.Hysteria2ConfigPath != "/etc/V2bX/hy2.json" {
		t.Errorf("Hysteria2ConfigPath lost: %q", got.Options.Hysteria2ConfigPath)
	}
	// customExt should still be in RawOptions after round-trip.
	var rawCheck map[string]interface{}
	if err := stdjson.Unmarshal(got.Options.RawOptions, &rawCheck); err != nil {
		t.Fatalf("unmarshal RawOptions: %v", err)
	}
	if rawCheck["customExt"] != "preserve-me" {
		t.Errorf("customExt unknown field dropped, got %v", rawCheck["customExt"])
	}
}

func TestNodeMarshal_EmptyCore_PreservesUnknownFields(t *testing.T) {
	src := []byte(`{
		"ApiHost":  "http://panel",
		"ApiKey":   "k",
		"NodeID":   7,
		"NodeType": "trojan",
		"Timeout":  30,
		"ListenIP": "0.0.0.0",
		"SendIP":   "0.0.0.0",
		"futureField": {"nested": [1, 2, 3]}
	}`)
	var n NodeConfig
	if err := stdjson.Unmarshal(src, &n); err != nil {
		t.Fatalf("unmarshal source: %v", err)
	}
	got := roundTrip(t, n)
	if got.Options.Core != "" {
		t.Errorf("Core should be empty, got %q", got.Options.Core)
	}
	var rawCheck map[string]interface{}
	if err := stdjson.Unmarshal(got.Options.RawOptions, &rawCheck); err != nil {
		t.Fatalf("unmarshal RawOptions: %v", err)
	}
	if rawCheck["futureField"] == nil {
		t.Errorf("futureField unknown field dropped")
	}
}

func TestNodeMarshal_OmitsZeroValueFields(t *testing.T) {
	n := NodeConfig{
		ApiConfig: ApiConfig{
			APIHost:  "http://panel",
			Key:      "k",
			NodeID:   1,
			NodeType: "vmess",
			Timeout:  30,
		},
		Options: Options{
			Core:     "xray",
			ListenIP: "0.0.0.0",
			SendIP:   "0.0.0.0",
			// Name, CoreName, Hysteria2ConfigPath, ReportMinTraffic,
			// DeviceOnlineMinTraffic all left at zero. LimitConfig also zero.
		},
	}
	data, err := stdjson.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	zeroFields := []string{`"Name"`, `"CoreName"`, `"Hysteria2ConfigPath"`,
		`"LimitConfig"`, `"ReportMinTraffic"`, `"DeviceOnlineMinTraffic"`,
		`"ApiSendIP"`, `"RuleListPath"`}
	for _, f := range zeroFields {
		if strings.Contains(s, f) {
			t.Errorf("zero-value field %s should be omitted, got: %s", f, s)
		}
	}
}

func TestNodeMarshal_LimitConfig_NonZeroEmitted(t *testing.T) {
	n := NodeConfig{
		ApiConfig: ApiConfig{APIHost: "h", Key: "k", NodeID: 1, NodeType: "t", Timeout: 30},
		Options: Options{
			Core:     "xray",
			ListenIP: "0.0.0.0",
			SendIP:   "0.0.0.0",
			LimitConfig: LimitConfig{
				EnableRealtime: true,
				SpeedLimit:     500,
				IPLimit:        2,
				ConnLimit:      100,
			},
		},
	}
	got := roundTrip(t, n)
	if !reflect.DeepEqual(got.Options.LimitConfig, n.Options.LimitConfig) {
		t.Errorf("LimitConfig lost: got %+v want %+v",
			got.Options.LimitConfig, n.Options.LimitConfig)
	}
}

func TestNodeMarshal_IncludeDirective_Preserved(t *testing.T) {
	// A node loaded with Include: when we Marshal it back, only the Include
	// directive should appear in the output.
	n := NodeConfig{OriginalInclude: "./nodes/node1.json"}
	data, err := stdjson.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"Include"`) ||
		!strings.Contains(string(data), `"./nodes/node1.json"`) {
		t.Errorf("expected Include directive preserved, got %s", data)
	}
	if strings.Contains(string(data), `"ApiHost"`) {
		t.Errorf("Include node should not emit inlined fields, got %s", data)
	}
}
