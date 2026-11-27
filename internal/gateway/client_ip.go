package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies parses a comma-separated list of IP addresses and CIDRs.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			p, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES: invalid CIDR %q", item)
			}
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: invalid IP %q", item)
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

// clientIP resolves the caller's IP: the TCP peer unless the peer is in trustedProxies,
// in which case X-Forwarded-For is walked right-to-left past trusted proxies.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	peerHost := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		peerHost = host
	}
	if len(trusted) == 0 {
		return peerHost
	}
	peerAddr, err := netip.ParseAddr(peerHost)
	if err != nil {
		return peerHost
	}
	peerAddr = peerAddr.Unmap()
	if !isTrusted(peerAddr, trusted) {
		return peerAddr.String()
	}
	xff := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if strings.TrimSpace(xff) == "" {
		return peerAddr.String()
	}
	parts := strings.Split(xff, ",")
	lastTrusted := peerAddr
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(parts[i])
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return lastTrusted.String()
		}
		addr = addr.Unmap()
		if !isTrusted(addr, trusted) {
			return addr.String()
		}
		lastTrusted = addr
	}
	return lastTrusted.String()
}

func isTrusted(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
