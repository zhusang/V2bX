package conf

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/InazumaV/V2bX/common/json5"
)

// rawConfigDoc captures the original byte representation of a top-level
// JSON object together with the byte ranges of each top-level field,
// allowing callers to swap individual field values while keeping the
// surrounding bytes (including JSON5 comments and unknown fields) intact.
type rawConfigDoc struct {
	original []byte
	objStart int
	objEnd   int
	fields   []rawField
	override map[string][]byte
}

type rawField struct {
	name       string
	keyStart   int
	valueStart int
	valueEnd   int
}

// parseRawConfig parses the byte content of a config file and records the
// position of every top-level field. It uses the json5 trim reader to
// neutralise comments without changing byte offsets, so positions in the
// trimmed view map 1:1 to the original bytes.
func parseRawConfig(data []byte) (*rawConfigDoc, error) {
	trimmed, err := io.ReadAll(json5.NewTrimNodeReader(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("strip comments: %w", err)
	}
	if len(trimmed) != len(data) {
		return nil, fmt.Errorf("internal: trimmed length %d != original %d", len(trimmed), len(data))
	}
	s := &byteScanner{data: trimmed}
	s.skipSpace()
	if !s.consume('{') {
		return nil, fmt.Errorf("expected '{' at root, got %q at offset %d", s.peek(), s.pos)
	}
	doc := &rawConfigDoc{
		original: data,
		objStart: s.pos - 1,
		override: make(map[string][]byte),
	}
	for {
		s.skipSpace()
		if s.consume('}') {
			doc.objEnd = s.pos
			return doc, nil
		}
		if len(doc.fields) > 0 {
			if !s.consume(',') {
				return nil, fmt.Errorf("expected ',' or '}' between fields at offset %d", s.pos)
			}
			s.skipSpace()
			if s.consume('}') {
				doc.objEnd = s.pos
				return doc, nil
			}
		}
		f := rawField{keyStart: s.pos}
		key, err := s.readString()
		if err != nil {
			return nil, fmt.Errorf("read top-level key: %w", err)
		}
		f.name = key
		s.skipSpace()
		if !s.consume(':') {
			return nil, fmt.Errorf("expected ':' after key %q at offset %d", key, s.pos)
		}
		s.skipSpace()
		f.valueStart = s.pos
		if err := s.skipValue(); err != nil {
			return nil, fmt.Errorf("read value of %q: %w", key, err)
		}
		f.valueEnd = s.pos
		doc.fields = append(doc.fields, f)
	}
}

// ReplaceField overrides the byte representation of the named field. If the
// field is not present in the original document, it is appended at the end
// of the object on Render.
func (d *rawConfigDoc) ReplaceField(name string, value []byte) {
	d.override[name] = value
}

// HasField reports whether the named top-level field exists in the original
// document.
func (d *rawConfigDoc) HasField(name string) bool {
	for _, f := range d.fields {
		if f.name == name {
			return true
		}
	}
	return false
}

// Render returns the resulting bytes after applying overrides. Unmodified
// fields are copied verbatim from the original input, preserving formatting,
// comments, and unknown fields.
func (d *rawConfigDoc) Render() []byte {
	var buf bytes.Buffer
	if len(d.fields) == 0 {
		buf.Write(d.original[:d.objStart+1])
		first := true
		for name, val := range d.override {
			if first {
				buf.WriteString("\n  ")
				first = false
			} else {
				buf.WriteString(",\n  ")
			}
			buf.WriteString(stdjsonQuote(name))
			buf.WriteString(": ")
			buf.Write(val)
		}
		if !first {
			buf.WriteString("\n")
		}
		buf.Write(d.original[d.objEnd-1:])
		return buf.Bytes()
	}

	// Copy bytes from start of file up to the first field's keyStart. This
	// captures the opening `{` and any leading whitespace / comments before
	// the first field.
	buf.Write(d.original[:d.fields[0].keyStart])

	known := make(map[string]bool, len(d.fields))
	for i, f := range d.fields {
		known[f.name] = true
		// Key bytes (key + colon + ws) are the bytes between keyStart and
		// valueStart in the original input.
		buf.Write(d.original[f.keyStart:f.valueStart])

		// Value: override if requested, otherwise verbatim from the original.
		if v, ok := d.override[f.name]; ok {
			buf.Write(v)
		} else {
			buf.Write(d.original[f.valueStart:f.valueEnd])
		}

		// Trailing region: from end of value to the start of the next field
		// (or end of file when this is the last field). This captures the
		// comma, surrounding whitespace, and any trailing comments.
		var nextStart int
		if i < len(d.fields)-1 {
			nextStart = d.fields[i+1].keyStart
		} else {
			nextStart = len(d.original)
		}
		buf.Write(d.original[f.valueEnd:nextStart])
	}

	// Handle overrides that introduce new top-level fields not present in the
	// original document. We need to insert them just before the closing `}`.
	var newFields []string
	for name := range d.override {
		if !known[name] {
			newFields = append(newFields, name)
		}
	}
	if len(newFields) == 0 {
		return buf.Bytes()
	}

	// Locate the position of the closing `}` in the bytes buffered so far.
	out := buf.Bytes()
	closeIdx := bytes.LastIndexByte(out, '}')
	if closeIdx < 0 {
		return out
	}
	prefix := out[:closeIdx]
	suffix := out[closeIdx:]

	// Trim trailing whitespace before the closing brace so the inserted
	// fields land cleanly. Then ensure the previous field's value is followed
	// by a comma.
	trimmed := bytes.TrimRight(prefix, " \t\r\n")
	hasContent := len(trimmed) > 0 && trimmed[len(trimmed)-1] != '{'

	var rebuilt bytes.Buffer
	rebuilt.Write(trimmed)
	for _, name := range newFields {
		if hasContent {
			rebuilt.WriteString(",")
		}
		rebuilt.WriteString("\n  ")
		rebuilt.WriteString(stdjsonQuote(name))
		rebuilt.WriteString(": ")
		rebuilt.Write(d.override[name])
		hasContent = true
	}
	rebuilt.WriteString("\n")
	rebuilt.Write(suffix)
	return rebuilt.Bytes()
}

// stdjsonQuote returns a JSON-quoted string. Falls back to a manual quote
// when Marshal somehow fails (it cannot for plain strings).
func stdjsonQuote(s string) string {
	b, err := stdjson.Marshal(s)
	if err != nil {
		return "\"" + s + "\""
	}
	return string(b)
}

// ----------------------------------------------------------------------------
// Atomic write helpers and rolling backup.
// ----------------------------------------------------------------------------

// atomicWriteFile writes data to filePath via a sibling temporary file plus
// rename, so that readers never observe a partial file. Uses 0644 perms.
func atomicWriteFile(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		return fmt.Errorf("chmod temp file %s: %w", tmpName, err)
	}
	if err := osRenameReplace(tmpName, filePath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, filePath, err)
	}
	cleanup = false
	return nil
}

// backupOriginal copies filePath to <filePath>.bak in an atomic-ish way (via
// <filePath>.bak.tmp + rename). Returns nil with no side effects when the
// original file does not exist (e.g., first-time deploy).
func backupOriginal(filePath string) error {
	src, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("backup: open %s: %w", filePath, err)
	}
	defer src.Close()

	bakPath := filePath + ".bak"
	dir := filepath.Dir(bakPath)
	tmp, err := os.CreateTemp(dir, filepath.Base(bakPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("backup: create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: fsync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("backup: close %s: %w", tmpName, err)
	}
	if err := osRenameReplace(tmpName, bakPath); err != nil {
		return fmt.Errorf("backup: rename %s -> %s: %w", tmpName, bakPath, err)
	}
	cleanup = false
	return nil
}

// osRenameReplace renames src to dst, replacing dst if it exists. On POSIX
// os.Rename already does this; on Windows older Go versions could fail when
// dst exists, so we fall back to remove + rename.
func osRenameReplace(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	// Best-effort fallback for platforms where Rename refuses to overwrite.
	if removeErr := os.Remove(dst); removeErr == nil {
		return os.Rename(src, dst)
	}
	return err
}

// ----------------------------------------------------------------------------
// Minimal JSON byte scanner used to locate top-level field byte ranges.
// Operates on the comment-stripped output of json5.NewTrimNodeReader, where
// comments have been replaced by spaces to keep byte offsets stable.
// ----------------------------------------------------------------------------

type byteScanner struct {
	data []byte
	pos  int
}

func (s *byteScanner) peek() byte {
	if s.pos >= len(s.data) {
		return 0
	}
	return s.data[s.pos]
}

func (s *byteScanner) consume(c byte) bool {
	if s.peek() == c {
		s.pos++
		return true
	}
	return false
}

func (s *byteScanner) skipSpace() {
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			s.pos++
			continue
		}
		break
	}
}

// readString consumes a JSON string token starting at the current position.
// Returns the unescaped content. Position advances past the closing quote.
func (s *byteScanner) readString() (string, error) {
	if s.peek() != '"' {
		return "", fmt.Errorf("expected '\"' at offset %d", s.pos)
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == '\\' {
			s.pos += 2
			continue
		}
		if c == '"' {
			s.pos++
			var out string
			if err := stdjson.Unmarshal(s.data[start:s.pos], &out); err != nil {
				return "", fmt.Errorf("decode string at %d: %w", start, err)
			}
			return out, nil
		}
		s.pos++
	}
	return "", fmt.Errorf("unterminated string starting at %d", start)
}

// skipValue advances the scanner past a single complete JSON value.
func (s *byteScanner) skipValue() error {
	s.skipSpace()
	if s.pos >= len(s.data) {
		return io.ErrUnexpectedEOF
	}
	switch s.data[s.pos] {
	case '{':
		return s.skipBalanced('{', '}')
	case '[':
		return s.skipBalanced('[', ']')
	case '"':
		_, err := s.readString()
		return err
	default:
		// number / true / false / null — read until a terminator.
		for s.pos < len(s.data) {
			c := s.data[s.pos]
			if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				return nil
			}
			s.pos++
		}
		return nil
	}
}

func (s *byteScanner) skipBalanced(open, close byte) error {
	if s.peek() != open {
		return fmt.Errorf("expected %q at %d", open, s.pos)
	}
	depth := 0
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		switch c {
		case '"':
			if _, err := s.readString(); err != nil {
				return err
			}
		case open:
			depth++
			s.pos++
		case close:
			depth--
			s.pos++
			if depth == 0 {
				return nil
			}
		default:
			s.pos++
		}
	}
	return fmt.Errorf("unterminated %q starting at %d", open, s.pos)
}
