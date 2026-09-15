package config

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// validate rejects unsafe combinations without rewriting operator policy.
func (c Config) validate() error {
	host, port, err := net.SplitHostPort(c.Server.Listen)
	ip, ipErr := netip.ParseAddr(host)
	n, portErr := strconv.Atoi(port)
	if err != nil || ipErr != nil || !ip.IsLoopback() || ip.Zone() != "" || portErr != nil || n < 1 || n > 65535 {
		return errors.New("config: server.listen requires a numeric loopback address and port 1..65535")
	}
	if c.Server.PublicBaseURL != "" && !validOrigin(c.Server.PublicBaseURL) {
		return errors.New("config: server.public_base_url must be an HTTP(S) origin")
	}
	if c.Provider.Type != "immich" {
		return errors.New("config: provider.type must be immich")
	}
	if !validOrigin(c.Provider.BaseURL) {
		return errors.New("config: provider.base_url must be an HTTP(S) origin without credentials, query or fragment")
	}
	if !filepath.IsAbs(c.Provider.APIKeyFile) || filepath.Clean(c.Provider.APIKeyFile) != c.Provider.APIKeyFile || unsafeText(c.Provider.APIKeyFile) {
		return errors.New("config: provider.api_key_file must be an absolute normalized file path")
	}
	if err := c.Policy.validate(); err != nil {
		return err
	}
	if c.Delivery.AllowOriginal || c.Delivery.ImageVariant != "preview" {
		return errors.New("config: delivery requires allow_original=false and image_variant=preview")
	}
	return nil
}

// validOrigin accepts only a configured HTTP authority, with an optional trailing slash.
func validOrigin(value string) bool {
	u, err := url.Parse(value)
	if err != nil || unsafeText(value) {
		return false
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return false
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	// Brackets are reserved for an IPv6 literal, not a malformed hostname.
	if strings.HasPrefix(u.Host, "[") || strings.ContainsAny(u.Hostname(), ":[]") {
		ip, err := netip.ParseAddr(u.Hostname())
		return err == nil && ip.Is6() && ip.Zone() == ""
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// unsafeText rejects ambiguous surrounding whitespace and control characters.
func unsafeText(value string) bool {
	return strings.TrimSpace(value) != value || strings.ContainsFunc(value, unicode.IsControl)
}
