package visitor

import (
	"net/netip"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
)

func TestVisitorID(t *testing.T) {
	h := NewHasher(config.NewSecret("0123456789abcdef0123456789abcdef"))
	ip := netip.MustParseAddr("203.0.113.7")
	day := time.Date(2026, 10, 14, 23, 59, 0, 0, time.UTC)
	a := h.VisitorID(ip, "UA", day)
	if len(a) != 64 || strings.Contains(a, "203.0.113.7") {
		t.Fatalf("bad id %q", a)
	}
	if a != h.VisitorID(ip, "UA", day.Add(-time.Hour)) {
		t.Fatal("same day must be stable")
	}
	if a == h.VisitorID(ip, "UA", day.Add(2*time.Minute)) {
		t.Fatal("must rotate daily")
	}
	if a == h.VisitorID(ip, "UA2", day) || a == h.VisitorID(netip.MustParseAddr("203.0.113.8"), "UA", day) {
		t.Fatal("must depend on ip and ua")
	}
	other := NewHasher(config.NewSecret("ffffffffffffffffffffffffffffffff"))
	if a == other.VisitorID(ip, "UA", day) {
		t.Fatal("must depend on salt")
	}
	if len(h.VisitorID(netip.Addr{}, "UA", day)) != 64 {
		t.Fatal("missing ip still hashes")
	}
}

func TestReferrerHostCountryNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"":                                       "",
		"https://News.YCombinator.com/item?id=1": "news.ycombinator.com",
		"https://t.co./abc":                      "t.co",
		"android-app://com.slack/":               "com.slack",
		"not a url %%":                           Unknown,
		"/relative/only":                         Unknown,
		"https://" + strings.Repeat("a", 80) + ".example/": strings.Repeat("a", 64),
	} {
		if got := ReferrerHost(in); got != want {
			t.Errorf("ReferrerHost(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"us": "US", "GB": "GB", "": Unknown, "USA": Unknown, "1A": Unknown, "A1": Unknown} {
		if got := Country(in); got != want {
			t.Errorf("Country(%q) = %q", in, got)
		}
	}
	if Normalize(" \x00\n ") != Unknown || Normalize("a\nb") != "ab" {
		t.Fatal("Normalize")
	}
	long := strings.Repeat("é", 40) // 80 bytes; cut must stay valid UTF-8
	if n := Normalize(long); !utf8.ValidString(n) || len(n) > MaxDimensionLen {
		t.Fatalf("Normalize long = %q", n)
	}
}

func TestTruncateUAAndBots(t *testing.T) {
	if TruncateUA("short") != "short" {
		t.Fatal("short UA unchanged")
	}
	long := strings.Repeat("ü", 200)
	if tr := TruncateUA(long); len(tr) > MaxUserAgentLen || !utf8.ValidString(tr) {
		t.Fatal("truncate")
	}
	bots := []string{"", "Googlebot/2.1", "curl/8.5.0", "Slackbot-LinkExpanding 1.0", "facebookexternalhit/1.1", "python-requests/2.32", "Mozilla/5.0 HeadlessChrome/120", "k6/2.3.0"}
	for _, ua := range bots {
		if !IsBot(ua) {
			t.Errorf("IsBot(%q) = false", ua)
		}
	}
	humans := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
	}
	for _, ua := range humans {
		if IsBot(ua) {
			t.Errorf("IsBot(%q) = true", ua)
		}
	}
}
