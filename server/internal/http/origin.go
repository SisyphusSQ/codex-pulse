package http

import (
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// RequestOrigin 只从真实 TLS 或明确可信代理确定入口，普通 forwarded header 无效。
func RequestOrigin(r *http.Request, cfg config.Config) (string, error) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		ip := net.ParseIP(remote)
		for _, proxy := range cfg.Server.TrustedProxies {
			_, prefix, err := net.ParseCIDR(proxy)
			if err != nil || !prefix.Contains(ip) {
				continue
			}
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				if proto != "http" && proto != "https" {
					return "", utils.ErrForbidden
				}
				scheme = proto
			}
			if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
				host = forwarded
			}
			break
		}
	}
	if strings.ContainsAny(host, ",\r\n/\\") || (scheme == "http" && !cfg.Server.AllowHTTP) {
		return "", utils.ErrForbidden
	}
	origin := scheme + "://" + host
	if !slices.Contains(cfg.Server.Origins, origin) {
		return "", utils.ErrForbidden
	}
	return origin, nil
}
