package links_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
)

func TestNormalizeURL(t *testing.T) {
	blocked := []string{"sho.rt", "d123.cloudfront.net"}
	tests := []struct {
		in, want string
		wantErr  string
	}{
		{in: "https://Example.COM/Path?q=1#frag", want: "https://example.com/Path?q=1#frag"},
		{in: "  HTTP://example.com:80/a  ", want: "http://example.com/a"},
		{in: "https://example.com:443", want: "https://example.com"},
		{in: "https://example.com:8443/x", want: "https://example.com:8443/x"},
		{in: "https://example.com./x", want: "https://example.com/x"},
		{in: "https://bücher.example/straße", want: "https://xn--bcher-kva.example/stra%C3%9Fe"},
		{in: "https://203.0.113.10/x", want: "https://203.0.113.10/x"},
		{in: "https://[2001:db8::1]:8080/", want: "https://[2001:db8::1]:8080/"},
		{in: "", wantErr: "url is required"},
		{in: "https://example.com/" + strings.Repeat("a", 2048), wantErr: "at most 2048"},
		{in: "ftp://example.com/x", wantErr: "scheme"},
		{in: "javascript:alert(1)", wantErr: "scheme"},
		{in: "https:///nohost", wantErr: "must have a host"},
		{in: "https://user:pass@example.com/", wantErr: "credentials"},
		{in: "https://user@example.com/", wantErr: "credentials"},
		{in: "http://10.1.2.3/", wantErr: "private"},
		{in: "http://192.168.0.1/", wantErr: "private"},
		{in: "http://127.0.0.1/", wantErr: "private"},
		{in: "http://169.254.169.254/latest/meta-data", wantErr: "private"},
		{in: "http://[::1]/", wantErr: "private"},
		{in: "http://[fe80::1%25eth0]/", wantErr: "private"},
		{in: "http://[::ffff:10.0.0.1]/", wantErr: "private"},
		{in: "http://100.64.1.1/", wantErr: "private"},
		{in: "http://0.0.0.0/", wantErr: "private"},
		{in: "http://224.0.0.1/", wantErr: "private"},
		{in: "http://127.1/", wantErr: "canonical"},
		{in: "http://0x7f.1/", wantErr: "canonical"},
		{in: "http://2130706433/", wantErr: "canonical"},
		{in: "http://localhost:8080/", wantErr: "public domain"},
		{in: "http://intranet/", wantErr: "public domain"},
		{in: "http://app.localhost/", wantErr: "public domain"},
		{in: "https://sho.rt/abc", wantErr: "this service"},
		{in: "https://www.SHO.rt/abc", wantErr: "this service"},
		{in: "https://d123.cloudfront.net/abc", wantErr: "this service"},
		{in: "https://notsho.rt/abc", want: "https://notsho.rt/abc"},
		{in: "https://exa mple.com/", wantErr: "parsed"},
		{in: "https://example.com:99999/", wantErr: "port"},
		{in: "https://xn--a.example/", wantErr: "valid domain"},
		{in: "https://.", wantErr: "must have a host"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := links.NormalizeURL(tt.in, blocked)
			if tt.wantErr != "" {
				var ve *links.ValidationError
				if !errors.As(err, &ve) || !errors.Is(err, links.ErrInvalidURL) || !strings.Contains(ve.Detail, tt.wantErr) {
					t.Fatalf("NormalizeURL(%q) err = %v, want detail containing %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizeURL(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestValidateAlias(t *testing.T) {
	for _, ok := range []string{"abcd", "My_Link-2026", strings.Repeat("a", 32)} {
		if err := links.ValidateAlias(ok); err != nil {
			t.Errorf("ValidateAlias(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"abc", strings.Repeat("a", 33), "has space", "dot.ted", "slash/x", "API", "Admin", "status", "healthz"} {
		if err := links.ValidateAlias(bad); !errors.Is(err, links.ErrInvalidInput) {
			t.Errorf("ValidateAlias(%q) = %v, want ErrInvalidInput", bad, err)
		}
	}
	if !links.IsReserved("Favicon.ico") || links.IsReserved("abcdef") {
		t.Fatal("IsReserved")
	}
	if !links.ValidCode("abc123") || links.ValidCode("") || links.ValidCode("a/b") || links.ValidCode(strings.Repeat("a", 33)) {
		t.Fatal("ValidCode")
	}
}

func TestValidateExpiry(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		exp  time.Time
		want string
	}{
		{now.Add(time.Hour), ""},
		{now.Add(links.MaxExpiry), ""},
		{now, "future"},
		{now.Add(-time.Hour), "future"},
		{now.Add(links.MaxExpiry + time.Second), "5 years"},
	}
	for _, c := range cases {
		err := links.ValidateExpiry(c.exp, now)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("ValidateExpiry(%v) = %v, want %q", c.exp, err, c.want)
		}
	}
}

func TestLinkExpired(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Second), now.Add(time.Second)
	if (links.Link{}).Expired(now) || !(links.Link{ExpiresAt: &past}).Expired(now) || (links.Link{ExpiresAt: &future}).Expired(now) || !(links.Link{ExpiresAt: &now}).Expired(now) {
		t.Fatal("Expired")
	}
}
