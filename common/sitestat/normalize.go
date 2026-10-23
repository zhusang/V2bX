package sitestat

import (
	"net"
	"strings"

	"golang.org/x/net/publicsuffix"
)

const (
	SiteUnknown = "(unknown)"
	SiteOther   = "(other)"
	SiteUDP     = "(udp)"

	GranularitySite = "site"
	GranularityHost = "host"

	// MaxSiteLen matches the panel's column and validation limit.
	MaxSiteLen = 253
)

// Normalize reduces a destination to a site key.
// Empty input returns an empty string. IP addresses are kept as-is.
// granularity "host" keeps the full hostname; anything else uses the
// registrable domain, falling back to the hostname when that fails.
func Normalize(target, granularity string) string {
	site := normalize(target, granularity)
	if len(site) > MaxSiteLen {
		site = site[:MaxSiteLen]
	}
	return site
}

func normalize(target, granularity string) string {
	host := strings.TrimSpace(target)
	if host == "" {
		return ""
	}
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host = strings.TrimSuffix(host, ".")

	if strings.HasPrefix(host, "[") {
		if j := strings.LastIndex(host, "]"); j >= 0 {
			ip := host[1:j]
			if parsed := net.ParseIP(ip); parsed != nil {
				return parsed.String()
			}
			return strings.ToLower(ip)
		}
	}

	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.String()
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.String()
	}
	if host == "" {
		return ""
	}
	if granularity == GranularityHost {
		return host
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || site == "" {
		return host
	}
	return site
}
