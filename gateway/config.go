package gateway

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Route struct {
	Path     string
	Methods  []string
	Upstream *url.URL
	re       *regexp.Regexp
}

type file struct {
	Routes []struct {
		Path     string   `yaml:"path"`
		Methods  []string `yaml:"methods"`
		Upstream string   `yaml:"upstream"`
	} `yaml:"routes"`
}

func Load(path string) ([]Route, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc file
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Routes) == 0 {
		return nil, fmt.Errorf("route list is empty")
	}
	seen := map[string]struct{}{}
	out := make([]Route, 0, len(doc.Routes))
	for _, item := range doc.Routes {
		rt, err := compile(item.Path, item.Methods, item.Upstream)
		if err != nil {
			return nil, err
		}
		for _, method := range rt.Methods {
			key := method + " " + rt.Path
			if _, ok := seen[key]; ok {
				return nil, fmt.Errorf("duplicate route %s", key)
			}
			seen[key] = struct{}{}
		}
		out = append(out, rt)
	}
	return out, nil
}

func compile(path string, methods []string, upstream string) (Route, error) {
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "?") {
		return Route{}, fmt.Errorf("bad route path %q", path)
	}
	if len(methods) == 0 {
		return Route{}, fmt.Errorf("route %s has no methods", path)
	}
	norm := make([]string, len(methods))
	for i, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			return Route{}, fmt.Errorf("route %s has an empty method", path)
		}
		norm[i] = method
	}
	if !strings.Contains(upstream, "://") {
		upstream = "http://" + upstream
	}
	u, err := url.Parse(upstream)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return Route{}, fmt.Errorf("route %s has bad upstream %q", path, upstream)
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	re, err := pathRegexp(path)
	if err != nil {
		return Route{}, err
	}
	return Route{Path: path, Methods: norm, Upstream: u, re: re}, nil
}

func pathRegexp(path string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(path); {
		if path[i] != '{' {
			b.WriteString(regexp.QuoteMeta(path[i : i+1]))
			i++
			continue
		}
		end := strings.IndexByte(path[i:], '}')
		if end <= 1 {
			return nil, fmt.Errorf("bad route path %q", path)
		}
		name := path[i+1 : i+end]
		if !validParam(name) {
			return nil, fmt.Errorf("bad route param %q", name)
		}
		b.WriteString("(?P<")
		b.WriteString(name)
		b.WriteString(">[^/]+)")
		i += end + 1
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func validParam(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func (rt Route) match(method, path string) (bool, map[string]string) {
	ok := false
	for _, m := range rt.Methods {
		if m == method {
			ok = true
			break
		}
	}
	if !ok {
		return false, nil
	}
	found := rt.re.FindStringSubmatch(path)
	if found == nil {
		return false, nil
	}
	params := map[string]string{}
	for i, name := range rt.re.SubexpNames() {
		if i > 0 && name != "" {
			params[name] = found[i]
		}
	}
	return true, params
}
