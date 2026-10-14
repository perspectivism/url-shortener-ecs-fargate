package links

import (
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

// MaxURLLength is the longest destination URL accepted (SPEC §10).
const MaxURLLength = 2048

// MaxExpiry is how far in the future an expiry may be (SPEC F3).
const MaxExpiry = 5 * 365 * 24 * time.Hour

var (
	aliasRE = regexp.MustCompile(`^[A-Za-z0-9_-]{4,32}$`)
	codeRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	cgnat   = netip.MustParsePrefix("100.64.0.0/10")
	// numericLabel matches a last host label that WHATWG URL parsing treats as
	// an IPv4 number (e.g. "127.1", "0x7f.1"); no real TLD looks like this.
	numericLabel = regexp.MustCompile(`^(0[xX][0-9a-fA-F]*|[0-9]+)$`)
)

// reserved codes are rejected as aliases and skipped by the generator (SPEC §6).
var reserved = map[string]bool{
	"api": true, "healthz": true, "admin": true, "static": true, "assets": true,
	"favicon.ico": true, "robots.txt": true, ".well-known": true, "docs": true,
	"login": true, "status": true,
}

// IsReserved reports whether code is reserved (case-insensitive).
func IsReserved(code string) bool { return reserved[strings.ToLower(code)] }

// ValidCode reports whether s is syntactically a possible short code. Used on
// the redirect path to reject junk before touching the cache or database.
func ValidCode(s string) bool { return codeRE.MatchString(s) }

// ValidateAlias checks a custom alias (SPEC F2).
func ValidateAlias(alias string) error {
	if !aliasRE.MatchString(alias) {
		return invalid(ErrInvalidInput, "alias must match [A-Za-z0-9_-]{4,32}")
	}
	if IsReserved(alias) {
		return invalid(ErrInvalidInput, "alias is reserved")
	}
	return nil
}

// ValidateExpiry checks an expiry is in the future and at most 5 years out (SPEC F3).
func ValidateExpiry(exp, now time.Time) error {
	if !exp.After(now) {
		return invalid(ErrInvalidInput, "expires_at must be in the future")
	}
	if exp.Sub(now) > MaxExpiry {
		return invalid(ErrInvalidInput, "expires_at must be at most 5 years in the future")
	}
	return nil
}

// NormalizeURL validates a destination URL and returns its canonical form
// (SPEC §10): http/https only, ≤ 2048 chars, a host, no userinfo, no private /
// loopback / link-local IP literals, not this service's own domain, IDN hosts
// converted to punycode, default ports removed.
func NormalizeURL(raw string, blockedHosts []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", invalid(ErrInvalidURL, "url is required")
	}
	if len(raw) > MaxURLLength {
		return "", invalid(ErrInvalidURL, "url must be at most 2048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", invalid(ErrInvalidURL, "url could not be parsed")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", invalid(ErrInvalidURL, "url scheme must be http or https")
	}
	if u.Opaque != "" || u.Host == "" {
		return "", invalid(ErrInvalidURL, "url must have a host")
	}
	if u.User != nil {
		return "", invalid(ErrInvalidURL, "url must not contain credentials")
	}

	host, err := normalizeHost(u.Hostname())
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", invalid(ErrInvalidURL, "url port is invalid")
		}
		if (u.Scheme == "http" && n == 80) || (u.Scheme == "https" && n == 443) {
			port = ""
		}
	}
	if isBlocked(host, blockedHosts) {
		return "", invalid(ErrInvalidURL, "url must not point to this service")
	}

	hostport := host
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if port != "" {
		hostport += ":" + port
	}
	u.Host = hostport
	out := u.String()
	if len(out) > MaxURLLength {
		return "", invalid(ErrInvalidURL, "url must be at most 2048 characters")
	}
	return out, nil
}

func normalizeHost(h string) (string, error) {
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return "", invalid(ErrInvalidURL, "url must have a host")
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		addr = addr.Unmap()
		if addr.Zone() != "" || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
			addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsMulticast() ||
			addr.IsUnspecified() || cgnat.Contains(addr) {
			return "", invalid(ErrInvalidURL, "url must not point to a private, loopback or link-local address")
		}
		return addr.String(), nil
	}
	if labels := strings.Split(h, "."); numericLabel.MatchString(labels[len(labels)-1]) {
		return "", invalid(ErrInvalidURL, "url host is not a canonical IP address or domain name")
	}
	ascii, err := idna.Lookup.ToASCII(strings.ToLower(h))
	if err != nil {
		return "", invalid(ErrInvalidURL, "url host is not a valid domain name")
	}
	if ascii == "localhost" || strings.HasSuffix(ascii, ".localhost") || !strings.Contains(ascii, ".") {
		return "", invalid(ErrInvalidURL, "url host must be a public domain name")
	}
	return ascii, nil
}

func isBlocked(host string, blocked []string) bool {
	for _, b := range blocked {
		b = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(b), "."))
		if b != "" && (host == b || strings.HasSuffix(host, "."+b)) {
			return true
		}
	}
	return false
}
