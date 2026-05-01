package conf

import (
	"bytes"
	"crypto/sha256"
	stdjson "encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureWithComments returns a JSON5 config file body containing comments,
// unknown fields, and inline Nodes (no Include — that case is exercised
// separately to avoid filesystem dependencies).
func fixtureWithComments() string {
	return `{
  // 顶层日志配置
  "Log": {
    "Level": "info",
    "Output": ""
  },
  /* future-extension: not parsed by current Conf, must be preserved */
  "ExperimentalKnob": {
    "version": 2
  },
  "Cores": [
    {
      "Type": "xray",
      "AssetPath": "/etc/V2bX/"
    }
  ],
  // 节点列表
  "Nodes": [
    {
      "Core": "xray",
      "ApiHost": "http://panel-a",
      "ApiKey": "ka",
      "NodeID": 1,
      "NodeType": "vmess",
      "Timeout": 30,
      "ListenIP": "0.0.0.0",
      "SendIP": "0.0.0.0",
      "EnableProxyProtocol": true,
      "EnableTFO": true,
      "DNSType": "AsIs",
      "LimitConfig": {
        "EnableRealtime": true,
        "SpeedLimit": 1000,
        "DeviceLimit": 3,
        "ConnLimit": 0,
        "EnableIpRecorder": false,
        "EnableDynamicSpeedLimit": false
      }
    }
  ]
}
`
}

// fixtureWithCommentsAndInclude is identical to fixtureWithComments except a
// second node uses the Include directive. The path is templated so callers
// can point it at a file they have created in a temp directory.
func fixtureWithCommentsAndInclude(includePath string) string {
	return `{
  // 顶层日志配置
  "Log": {
    "Level": "info"
  },
  // 节点列表
  "Nodes": [
    {
      "Core": "xray",
      "ApiHost": "http://panel-a",
      "ApiKey": "ka",
      "NodeID": 1,
      "NodeType": "vmess",
      "Timeout": 30,
      "ListenIP": "0.0.0.0",
      "SendIP": "0.0.0.0"
    },
    {"Include": "` + includePath + `"}
  ]
}
`
}

func TestParseRawConfig_TopLevelFields(t *testing.T) {
	data := []byte(fixtureWithComments())
	doc, err := parseRawConfig(data)
	if err != nil {
		t.Fatalf("parseRawConfig: %v", err)
	}
	want := []string{"Log", "ExperimentalKnob", "Cores", "Nodes"}
	if len(doc.fields) != len(want) {
		t.Fatalf("expected %d fields, got %d: %+v", len(want), len(doc.fields), doc.fields)
	}
	for i, name := range want {
		if doc.fields[i].name != name {
			t.Errorf("field %d: got %q want %q", i, doc.fields[i].name, name)
		}
	}
}

func TestRawConfigDoc_ReplaceField_PreservesEverythingElse(t *testing.T) {
	data := []byte(fixtureWithComments())
	doc, err := parseRawConfig(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	doc.ReplaceField("Nodes", []byte(`["NEW"]`))
	out := doc.Render()
	s := string(out)

	// Top-level untouched fields & comments must remain intact.
	mustContain := []string{
		`// 顶层日志配置`,
		`"Log"`,
		`"Level": "info"`,
		`/* future-extension: not parsed by current Conf, must be preserved */`,
		`"ExperimentalKnob"`,
		`"version": 2`,
		`"Cores"`,
		`// 节点列表`,
		`["NEW"]`, // new Nodes value
	}
	for _, sub := range mustContain {
		if !strings.Contains(s, sub) {
			t.Errorf("output missing %q\n--- output ---\n%s", sub, s)
		}
	}
	// The original first node (xray) should NOT appear since Nodes was replaced.
	if strings.Contains(s, `"http://panel-a"`) {
		t.Errorf("output should have replaced Nodes content but still has old node bytes")
	}
}

func TestRawConfigDoc_NoOverride_EqualsOriginal(t *testing.T) {
	data := []byte(fixtureWithComments())
	doc, err := parseRawConfig(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := doc.Render()
	if !bytes.Equal(out, data) {
		t.Errorf("Render() with no override differs from original.\n--- got ---\n%s\n--- want ---\n%s",
			out, data)
	}
}

func TestRawConfigDoc_ReplaceField_AddsMissingField(t *testing.T) {
	src := `{
  "Log": {"Level": "info"},
  "Cores": []
}
`
	doc, err := parseRawConfig([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	doc.ReplaceField("Nodes", []byte(`[{"NodeID": 1}]`))
	out := string(doc.Render())
	if !strings.Contains(out, `"Nodes"`) {
		t.Errorf("missing Nodes field after override:\n%s", out)
	}
	if !strings.Contains(out, `"NodeID": 1`) {
		t.Errorf("Nodes value missing:\n%s", out)
	}
}

func TestSave_PreservesUntouchedFieldsAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(fixtureWithComments()), 0644); err != nil {
		t.Fatal(err)
	}

	c := New()
	if err := c.LoadFromPath(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Append a brand-new node and save.
	c.NodeConfig = append(c.NodeConfig, NodeConfig{
		ApiConfig: ApiConfig{
			APIHost: "http://panel-b", Key: "kb", NodeID: 2,
			NodeType: "trojan", Timeout: 30,
		},
		Options: Options{Core: "xray", ListenIP: "0.0.0.0", SendIP: "0.0.0.0",
			XrayOptions: NewXrayOptions(), CertConfig: NewCertConfig()},
	})
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(saved)

	mustContain := []string{
		`// 顶层日志配置`,
		`/* future-extension: not parsed by current Conf, must be preserved */`,
		`"ExperimentalKnob"`,
		`"version": 2`,
		`// 节点列表`,
	}
	for _, sub := range mustContain {
		if !strings.Contains(s, sub) {
			t.Errorf("Save lost top-level content: missing %q\n%s", sub, s)
		}
	}
}

func TestSave_PreservesIncludeDirective(t *testing.T) {
	dir := t.TempDir()
	includePath := filepath.Join(dir, "extra.json")
	includeBody := `{
  "Core": "xray",
  "ApiHost": "http://panel-included",
  "ApiKey": "ki",
  "NodeID": 99,
  "NodeType": "vmess",
  "Timeout": 30,
  "ListenIP": "0.0.0.0",
  "SendIP": "0.0.0.0"
}`
	if err := os.WriteFile(includePath, []byte(includeBody), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "config.json")
	// Use forward slashes in the fixture so JSON parsing doesn't have to
	// deal with Windows backslash escapes.
	posixIncludePath := strings.ReplaceAll(includePath, `\`, `/`)
	if err := os.WriteFile(mainPath, []byte(fixtureWithCommentsAndInclude(posixIncludePath)), 0644); err != nil {
		t.Fatal(err)
	}

	c := New()
	if err := c.LoadFromPath(mainPath); err != nil {
		t.Fatalf("load: %v", err)
	}
	// Append a fresh node so Save has to re-emit the Nodes array.
	c.NodeConfig = append(c.NodeConfig, NodeConfig{
		ApiConfig: ApiConfig{APIHost: "h", Key: "k", NodeID: 5, NodeType: "vmess", Timeout: 30},
		Options:   Options{Core: "xray", ListenIP: "0.0.0.0", SendIP: "0.0.0.0", CertConfig: NewCertConfig()},
	})
	if err := c.Save(mainPath); err != nil {
		t.Fatalf("save: %v", err)
	}
	saved, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(saved)
	if !strings.Contains(s, `"Include"`) {
		t.Errorf("Include directive lost after Save:\n%s", s)
	}
	if !strings.Contains(s, posixIncludePath) {
		t.Errorf("Include path lost after Save:\n%s", s)
	}
	// The included file body MUST NOT be inlined into the main config.
	if strings.Contains(s, `"http://panel-included"`) {
		t.Errorf("Include was inlined instead of preserved as directive:\n%s", s)
	}
	// The included file MUST NOT have been overwritten.
	includedBack, err := os.ReadFile(includePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(includedBack) != includeBody {
		t.Errorf("included file was modified by Save\n--- before ---\n%s\n--- after ---\n%s",
			includeBody, includedBack)
	}
}

func TestSave_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(fixtureWithComments())
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	originalHash := sha256.Sum256(original)

	c := New()
	if err := c.LoadFromPath(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	bakHash := sha256.Sum256(bak)
	if bakHash != originalHash {
		t.Errorf(".bak hash differs from original config hash")
	}
}

func TestSave_BackupRollsOverEachWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(fixtureWithComments()), 0644); err != nil {
		t.Fatal(err)
	}

	c := New()
	if err := c.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}

	// First Save: backup contains the original fixture.
	if err := c.Save(path); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	first, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the in-memory state and save again. Backup should now contain
	// the result of the first save (i.e., the file just before the second write),
	// NOT the original fixture.
	c.NodeConfig = append(c.NodeConfig, NodeConfig{
		ApiConfig: ApiConfig{APIHost: "h", Key: "k", NodeID: 99, NodeType: "x", Timeout: 30},
		Options:   Options{Core: "xray", ListenIP: "0.0.0.0", SendIP: "0.0.0.0", CertConfig: NewCertConfig()},
	})
	stateBeforeSecond, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	second, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Errorf("backup did not roll over between writes")
	}
	if !bytes.Equal(second, stateBeforeSecond) {
		t.Errorf(".bak after second save should equal main file before second save")
	}
}

func TestSave_OriginalMissing_NoBackupNoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := New()
	c.NodeConfig = []NodeConfig{}
	// Save when file does not exist: should create new file, no .bak file.
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("backup file should NOT exist on first-time write, stat err = %v", err)
	}
}

func TestSave_NodesArrayIncrementalUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(fixtureWithComments()), 0644); err != nil {
		t.Fatal(err)
	}

	c := New()
	if err := c.LoadFromPath(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Capture the original node count and limit settings.
	origCount := len(c.NodeConfig)
	if origCount < 1 {
		t.Fatalf("fixture should have at least one node, got %d", origCount)
	}
	origLimit := c.NodeConfig[0].Options.LimitConfig

	// Append a node and save.
	c.NodeConfig = append(c.NodeConfig, NodeConfig{
		ApiConfig: ApiConfig{APIHost: "h", Key: "k", NodeID: 100, NodeType: "trojan", Timeout: 30},
		Options:   Options{Core: "xray", ListenIP: "0.0.0.0", SendIP: "0.0.0.0", CertConfig: NewCertConfig()},
	})
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload and verify the original node still has its LimitConfig.
	c2 := New()
	if err := c2.LoadFromPath(path); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(c2.NodeConfig) != origCount+1 {
		t.Errorf("expected %d nodes after add, got %d", origCount+1, len(c2.NodeConfig))
	}
	if c2.NodeConfig[0].Options.LimitConfig.SpeedLimit != origLimit.SpeedLimit {
		t.Errorf("first node LimitConfig.SpeedLimit changed: %d -> %d",
			origLimit.SpeedLimit, c2.NodeConfig[0].Options.LimitConfig.SpeedLimit)
	}
	if c2.NodeConfig[0].Options.LimitConfig.IPLimit != origLimit.IPLimit {
		t.Errorf("first node LimitConfig.IPLimit changed: %d -> %d",
			origLimit.IPLimit, c2.NodeConfig[0].Options.LimitConfig.IPLimit)
	}
}

func TestParseRawConfig_InvalidJSONReturnsError(t *testing.T) {
	_, err := parseRawConfig([]byte(`{"unterminated":`))
	if err == nil {
		t.Errorf("expected error on invalid JSON, got nil")
	}
}

func TestSave_InvalidExistingFile_DoesNotTouchOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	bad := []byte("not valid json at all {{{")
	if err := os.WriteFile(path, bad, 0644); err != nil {
		t.Fatal(err)
	}

	c := New()
	c.NodeConfig = []NodeConfig{
		{ApiConfig: ApiConfig{APIHost: "h", Key: "k", NodeID: 1, NodeType: "t", Timeout: 30},
			Options: Options{Core: "xray", ListenIP: "0.0.0.0", SendIP: "0.0.0.0"}},
	}
	if err := c.Save(path); err == nil {
		t.Errorf("expected Save to fail on invalid existing file, got nil")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, bad) {
		t.Errorf("original file modified despite parse error.\noriginal: %s\nnow: %s", bad, got)
	}
}

func TestBackupOriginal_FileMissing_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	if err := backupOriginal(path); err != nil {
		t.Errorf("expected nil for missing original, got %v", err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("backup should not exist when original is missing")
	}
}

// Sanity check: make sure the json tag mapping for top-level Conf matches
// the field names used by parseRawConfig.
func TestConf_TopLevelKeyNames(t *testing.T) {
	c := New()
	c.NodeConfig = []NodeConfig{}
	data, err := stdjson.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"Log"`, `"Cores"`, `"Nodes"`} {
		if !strings.Contains(string(data), k) {
			t.Errorf("top-level key %s missing from default Conf marshal: %s", k, data)
		}
	}
}
