package conf

import (
	stdjson "encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/InazumaV/V2bX/common/json5"

	"encoding/json/v2"
)

type Conf struct {
	LogConfig   LogConfig    `json:"Log"`
	CoresConfig []CoreConfig `json:"Cores"`
	NodeConfig  []NodeConfig `json:"Nodes"`
}

func New() *Conf {
	return &Conf{
		LogConfig: LogConfig{
			Level:  "info",
			Output: "",
		},
	}
}

func (p *Conf) LoadFromPath(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open config file error: %s", err)
	}
	defer f.Close()

	reader := json5.NewTrimNodeReader(f)
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read config file error: %s", err)
	}

	err = json.Unmarshal(data, p)
	if err != nil {
		return fmt.Errorf("unmarshal config error: %s", err)
	}

	return nil
}

// Save persists the in-memory Conf to filePath using incremental write
// semantics: it only rewrites the `Nodes` field of the original file and
// preserves every other top-level field (including its formatting,
// comments, and unknown keys). The original file is first copied to
// `<filePath>.bak` as a rolling single-file backup; if the backup step
// fails, the write is aborted and the original file is left untouched.
//
// If filePath does not yet exist (first deploy), Save degenerates into
// OverwriteSave and creates the file from scratch without any backup.
func (p *Conf) Save(filePath string) error {
	// Backup MUST run before any write operation. fail-fast on errors so
	// the original file (if any) is never left without a recovery point.
	if err := backupOriginal(filePath); err != nil {
		return err
	}

	original, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// First-time write: fall back to full overwrite. backupOriginal
			// is a no-op in this case so nothing has been touched yet.
			return p.OverwriteSave(filePath)
		}
		return fmt.Errorf("read original config: %w", err)
	}

	doc, err := parseRawConfig(original)
	if err != nil {
		return fmt.Errorf("parse original config: %w", err)
	}

	nodesValue, err := stdjson.MarshalIndent(p.NodeConfig, "  ", "  ")
	if err != nil {
		return fmt.Errorf("marshal nodes: %w", err)
	}
	doc.ReplaceField("Nodes", nodesValue)

	out := doc.Render()
	if err := atomicWriteFile(filePath, out); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// OverwriteSave serializes the entire Conf and replaces the file content
// in full. It is the legacy "fully rewrite" behaviour and should only be
// used when callers explicitly want to discard the original file's
// formatting, comments, and unknown fields.
//
// OverwriteSave still performs a rolling backup before writing, so the
// previous content is recoverable from `<filePath>.bak`.
func (p *Conf) OverwriteSave(filePath string) error {
	if err := backupOriginal(filePath); err != nil {
		return err
	}
	data, err := stdjson.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config error: %s", err)
	}
	if err := atomicWriteFile(filePath, data); err != nil {
		return fmt.Errorf("write config file error: %s", err)
	}
	return nil
}
