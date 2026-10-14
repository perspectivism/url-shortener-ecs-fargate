// Package visitor derives privacy-preserving click attributes (ADR-0018):
// an HMAC visitor ID that rotates daily, the referrer host only, the viewer
// country, and a bot heuristic. Raw IPs never leave the redirect service.
package visitor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
)

// MaxDimensionLen caps stored dimension values (SPEC §5.2).
const MaxDimensionLen = 64

// MaxUserAgentLen caps the user agent carried in click events (SPEC §8.1).
const MaxUserAgentLen = 256

// Unknown is the normalized value for missing or unparseable dimensions.
const Unknown = "unknown"

// Hasher computes visitor IDs.
type Hasher struct{ salt []byte }

// NewHasher returns a Hasher keyed by the visitor-hash-salt secret.
func NewHasher(salt config.Secret) *Hasher { return &Hasher{salt: []byte(salt.Reveal())} }

// VisitorID returns hex(HMAC-SHA256(salt, ip | user_agent | yyyy-mm-dd)). The
// date makes IDs unlinkable across days; the keyed hash makes them
// irreversible without the salt (an unkeyed hash of an IPv4 is brute-forceable).
func (h *Hasher) VisitorID(ip netip.Addr, ua string, t time.Time) string {
	m := hmac.New(sha256.New, h.salt)
	ipStr := ""
	if ip.IsValid() {
		ipStr = ip.String()
	}
	_, _ = m.Write([]byte(ipStr + "|" + ua + "|" + t.UTC().Format(time.DateOnly)))
	return hex.EncodeToString(m.Sum(nil))
}

// ReferrerHost returns only the lowercase host of a Referer header value.
// Empty means a direct visit; anything unparseable is Unknown.
func ReferrerHost(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Hostname() == "" {
		return Unknown
	}
	return Normalize(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."))
}

// Country returns the ISO 3166-1 alpha-2 code from CloudFront-Viewer-Country, or Unknown.
func Country(h string) string {
	h = strings.ToUpper(strings.TrimSpace(h))
	if len(h) != 2 || h[0] < 'A' || h[0] > 'Z' || h[1] < 'A' || h[1] > 'Z' {
		return Unknown
	}
	return h
}

// Normalize trims, strips control characters and caps a dimension value.
func Normalize(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if v == "" {
		return Unknown
	}
	if len(v) > MaxDimensionLen {
		v = strings.ToValidUTF8(v[:MaxDimensionLen], "")
	}
	return v
}

// TruncateUA caps a user agent at MaxUserAgentLen bytes (valid UTF-8).
func TruncateUA(ua string) string {
	if len(ua) <= MaxUserAgentLen {
		return ua
	}
	return strings.ToValidUTF8(ua[:MaxUserAgentLen], "")
}

var botMarkers = []string{
	"bot", "crawl", "spider", "slurp", "preview", "fetch", "monitor", "scan",
	"curl/", "wget/", "python-requests", "python-urllib", "go-http-client", "okhttp",
	"java/", "libwww", "httpclient", "axios/", "node-fetch", "headlesschrome",
	"facebookexternalhit", "whatsapp", "telegram", "lighthouse", "pingdom", "k6/",
}

// IsBot is a cheap user-agent heuristic. Bots count toward bot_clicks and are
// excluded from unique visitors. Empty user agents are treated as bots.
func IsBot(ua string) bool {
	if strings.TrimSpace(ua) == "" {
		return true
	}
	l := strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}
