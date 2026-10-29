package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The config the tests apply: Plat5's e2e config, from the plat5 repo, with operator's
// additions merged in. See README.md.
const (
	plat5Config     = "e2e/testdata/config.json" // inside the plat5 repo
	additionsConfig = "testdata/additions.json"  // inside this module
)

// plat5Dir is the plat5 repo: PLAT5_DIR, or ../plat5 beside this repo. A relative PLAT5_DIR
// is read from the operator repo root, the parent of this module (go test runs in e2e/).
func plat5Dir() string {
	dir := env("PLAT5_DIR", "../plat5")
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join("..", dir)
}

// loadConfig reads Plat5's e2e config from the plat5 repo at dir and merges the additions
// file into it.
func loadConfig(dir, additionsPath string) (map[string]any, error) {
	path := filepath.Join(dir, plat5Config)
	base, err := readObject(path)
	if errors.Is(err, fs.ErrNotExist) {
		abs, _ := filepath.Abs(path)
		return nil, fmt.Errorf("Plat5's e2e config isn't at %s: clone plat5 beside operator (../plat5), or set PLAT5_DIR to the plat5 repo", abs)
	}
	if err != nil {
		return nil, err
	}
	add, err := readObject(additionsPath)
	if err != nil {
		return nil, err
	}
	return merge(base, add), nil
}

func readObject(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if out == nil {
		return nil, fmt.Errorf("%s: not a JSON object", path)
	}
	return out, nil
}

// merge returns base with add merged in, changing neither. A key in both whose values are
// both objects merges the same way, key by key. Otherwise add's value wins whole: an array,
// string, number, boolean or null replaces base's value, and arrays are never merged element
// by element. Keys only in base are kept; keys only in add are added.
func merge(base, add map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(add))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range add {
		bm, bok := out[k].(map[string]any)
		am, aok := v.(map[string]any)
		if bok && aok {
			out[k] = merge(bm, am)
		} else {
			out[k] = v
		}
	}
	return out
}
