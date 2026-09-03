// Package configfile writes edited values back into an app's on-disk config.json.
//
// The suite's infrastructure blocks (db, cache, server, tls, logging, rate limit, ...)
// are read by infra/apphost exactly ONCE at boot, before any app is handed a database
// handle. There is no seam for a stored value to override them at startup, so the only
// way an edit can take effect is to write it into the file the host re-reads on the next
// launch. Two callers need that: the Settings screen (edit, then restart) and the
// first-boot setup wizard (edit, then continue booting in the same process).
//
// The write is surgical. Only the top-level blocks named by the patches are
// re-serialized; every other block keeps its original bytes verbatim, so an untouched
// block never reformats and diffs stay small. The original top-level key ORDER is
// preserved for the same reason. Writes are atomic (temp file + rename), so a crash
// mid-write can never leave a truncated config.json — the file that, if lost, means the
// app cannot boot at all.
package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Patch sets one leaf value at a nested path, e.g. {["rateLimit","devOnly","requests"], 300}.
// A single-element path sets a top-level scalar (e.g. {["allowOrigins"], "*"}).
type Patch struct {
	Path  []string
	Value any
}

// Flatten turns a nested map into leaf Patches. Scalars and non-map values become one
// patch each; nested maps recurse. prefix is the root path (nil at the top). Keys are
// walked in sorted order so a given input always produces the same patch list.
func Flatten(prefix []string, data map[string]any) []Patch {
	var patches []Patch
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		path := append(append([]string{}, prefix...), k)
		if child, ok := data[k].(map[string]any); ok {
			patches = append(patches, Flatten(path, child)...)
			continue
		}
		patches = append(patches, Patch{Path: path, Value: data[k]})
	}
	return patches
}

// Materialize applies patches to the config document at configPath and writes it back
// atomically. An empty patch list is a no-op (no file read, no file write), so a caller
// that computed no changes never touches the file.
func Materialize(configPath string, patches []Patch) error {
	if len(patches) == 0 {
		return nil
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	out, err := PatchBytes(raw, patches)
	if err != nil {
		return err
	}
	return AtomicWrite(configPath, out)
}

// PatchBytes returns raw with patches applied. Only the top-level blocks named by the
// patches are re-serialized; all other top-level entries keep their original bytes.
func PatchBytes(raw []byte, patches []Patch) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Group patches by their top-level key.
	byTop := map[string][]Patch{}
	for _, p := range patches {
		if len(p.Path) == 0 {
			continue
		}
		byTop[p.Path[0]] = append(byTop[p.Path[0]], p)
	}

	for key, ps := range byTop {
		// A single-element path replaces a top-level scalar wholesale.
		if len(ps) == 1 && len(ps[0].Path) == 1 {
			vb, err := json.MarshalIndent(ps[0].Value, "  ", "  ")
			if err != nil {
				return nil, err
			}
			top[key] = vb
			continue
		}
		// Otherwise the key is an object block: decode it, set the nested leaves,
		// and re-serialize just that block.
		var block map[string]any
		if rb, ok := top[key]; ok && len(rb) > 0 {
			if err := json.Unmarshal(rb, &block); err != nil {
				return nil, fmt.Errorf("parse config block %q: %w", key, err)
			}
		}
		if block == nil {
			block = map[string]any{}
		}
		for _, p := range ps {
			SetPath(block, p.Path[1:], p.Value)
		}
		vb, err := json.MarshalIndent(block, "  ", "  ")
		if err != nil {
			return nil, err
		}
		top[key] = vb
	}

	return encodeOrdered(raw, top)
}

// SetPath walks (creating intermediate objects as needed) and sets the leaf value.
func SetPath(root map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	cur := root
	for i := 0; i < len(path)-1; i++ {
		next, ok := cur[path[i]].(map[string]any)
		if !ok || next == nil {
			next = map[string]any{}
			cur[path[i]] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = value
}

// encodeOrdered stitches the (possibly patched) top-level blocks back into a pretty JSON
// document, emitting keys in the original document order (new keys appended sorted). Each
// block's bytes are used verbatim, so blocks that were not re-serialized keep their exact
// original formatting.
func encodeOrdered(raw []byte, top map[string]json.RawMessage) ([]byte, error) {
	order := TopLevelKeyOrder(raw)
	seen := make(map[string]bool, len(order))
	keys := make([]string, 0, len(top))
	for _, k := range order {
		if _, ok := top[k]; ok && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	extra := make([]string, 0)
	for k := range top {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.WriteString("  ")
		buf.Write(kb)
		buf.WriteString(": ")
		buf.Write(bytes.TrimSpace(top[k]))
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// TopLevelKeyOrder returns the object keys of the top-level JSON object in document order.
// It interleaves Token() (to read each key) with Decode() (to skip that key's whole value),
// which the standard decoder supports.
func TopLevelKeyOrder(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // consume the opening '{'
		return nil
	}
	var order []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := keyTok.(string)
		if !ok {
			break
		}
		order = append(order, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			break
		}
	}
	return order
}

// AtomicWrite writes data to a temp file in the same directory and renames it over path,
// so a crash mid-write never leaves a truncated config.json. os.Rename replaces the
// destination on both POSIX and Windows.
func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kopiv2-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
