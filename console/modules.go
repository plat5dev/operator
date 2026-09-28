package console

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Module struct {
	ID       string `json:"id" yaml:"id"`
	Title    string `json:"title" yaml:"title"`
	BasePath string `json:"basePath" yaml:"base_path"`
	Entry    string `json:"entry" yaml:"entry"`
}

func LoadModules(path string) ([]Module, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Modules []Module `yaml:"modules"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Modules == nil {
		doc.Modules = []Module{}
	}
	seenID := map[string]struct{}{}
	seenPath := map[string]struct{}{}
	for i := range doc.Modules {
		item := &doc.Modules[i]
		item.ID = strings.TrimSpace(item.ID)
		item.Title = strings.TrimSpace(item.Title)
		item.BasePath = strings.TrimRight(strings.TrimSpace(item.BasePath), "/")
		item.Entry = strings.TrimSpace(item.Entry)
		if err := validModule(*item); err != nil {
			return nil, err
		}
		if _, ok := seenID[item.ID]; ok {
			return nil, fmt.Errorf("duplicate module %s", item.ID)
		}
		seenID[item.ID] = struct{}{}
		if _, ok := seenPath[item.BasePath]; ok {
			return nil, fmt.Errorf("duplicate module path %s", item.BasePath)
		}
		seenPath[item.BasePath] = struct{}{}
	}
	for i, a := range doc.Modules {
		for j, b := range doc.Modules {
			if i != j && strings.HasPrefix(b.BasePath, a.BasePath+"/") {
				return nil, fmt.Errorf("module path %s overlaps %s", b.BasePath, a.BasePath)
			}
		}
	}
	return doc.Modules, nil
}

func validModule(item Module) error {
	if !validID(item.ID) {
		return fmt.Errorf("bad module id %q", item.ID)
	}
	if item.Title == "" || len(item.Title) > 128 {
		return fmt.Errorf("module %s has a bad title", item.ID)
	}
	if !validBasePath(item.BasePath) {
		return fmt.Errorf("module %s has a bad path %q", item.ID, item.BasePath)
	}
	if !validEntry(item.Entry) {
		return fmt.Errorf("module %s has a bad entry %q", item.ID, item.Entry)
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '-' || c == '_' || c == '.'):
		default:
			return false
		}
	}
	return true
}

var reservedPaths = []string{
	"/login", "/account", "/session", "/logout", "/health",
	"/modules", "/vendor", "/assets",
	"/users", "/organizations", "/members",
}

func validBasePath(base string) bool {
	if base == "" || base == "/" || !strings.HasPrefix(base, "/") || strings.HasPrefix(base, "//") {
		return false
	}
	if strings.Contains(base, "..") || strings.ContainsAny(base, "?#\\") {
		return false
	}
	for _, reserved := range reservedPaths {
		if base == reserved || strings.HasPrefix(base, reserved+"/") {
			return false
		}
	}
	return true
}

func validEntry(entry string) bool {
	if !strings.HasPrefix(entry, "/modules/") || strings.HasPrefix(entry, "//") || strings.HasSuffix(entry, "/") {
		return false
	}
	if strings.Contains(entry, "..") || strings.ContainsAny(entry, "?#\\") {
		return false
	}
	return strings.TrimPrefix(entry, "/modules/") != ""
}

func modulesJSON(modules []Module) string {
	if modules == nil {
		modules = []Module{}
	}
	raw, err := json.Marshal(modules)
	if err != nil {
		return "[]"
	}
	var buf bytes.Buffer
	json.HTMLEscape(&buf, raw)
	return buf.String()
}
