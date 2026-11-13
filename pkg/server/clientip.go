package server

import (
	"net"
	"net/http"
	"strings"
)

// ParseTrustedProxies parses IPs / CIDRs of reverse proxies whose
// X-Forwarded-For / X-Real-IP headers are trusted.
func ParseTrustedProxies(list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, item := range list {
		for _, p := range strings.Split(item, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !strings.Contains(p, "/") {
				if ip := net.ParseIP(p); ip != nil && ip.To4() != nil {
					p += "/32"
				} else {
					p += "/128"
				}
			}
			_, n, err := net.ParseCIDR(p)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
	}
	return out, nil
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) trusted(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range s.cfg.TrustedProxies {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// clientIP returns the real client address. Forwarding headers are only
// honoured when the connection comes from a trusted proxy; X-Forwarded-For is
// walked from the right, skipping trusted hops, so a client cannot spoof it.
func (s *Server) clientIP(r *http.Request) string {
	ip := remoteIP(r)
	if len(s.cfg.TrustedProxies) == 0 || !s.trusted(ip) {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			h := strings.TrimSpace(hops[i])
			if net.ParseIP(h) == nil {
				break
			}
			if !s.trusted(h) {
				return h
			}
			ip = h
		}
		return ip
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(xr) != nil {
		return xr
	}
	return ip
}
