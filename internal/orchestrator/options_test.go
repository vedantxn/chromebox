package orchestrator

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/vedantxn/chromebox/internal/gateway"
)

func TestSessionOptionsAreChecked(t *testing.T) {
	good := []SessionOptions{
		{},
		{Viewport: &gateway.Viewport{Width: 1280, Height: 800}, UserAgent: "Mozilla/5.0 test", Locale: "fr-FR", Timezone: "Europe/Paris", TimeoutMinutes: 60},
		{Timezone: "UTC", Locale: "en"},
		{Proxy: &gateway.Upstream{Server: "proxy.example.com:8080", Username: "u", Password: "p"}},
		{Proxy: &gateway.Upstream{Server: "http://203.0.113.10:3128"}},
	}
	for _, s := range good {
		if err := s.validate(); err != nil {
			t.Errorf("%+v refused: %v", s, err)
		}
	}
	bad := map[string]SessionOptions{
		"viewport":         {Viewport: &gateway.Viewport{Width: 100, Height: 800}},
		"user agent":       {UserAgent: "a\r\nX-Injected: 1"},
		"locale":           {Locale: "french please"},
		"timezone":         {Timezone: "Mars/Olympus"},
		"timeout":          {TimeoutMinutes: 61},
		"proxy no port":    {Proxy: &gateway.Upstream{Server: "proxy.example.com"}},
		"proxy metadata":   {Proxy: &gateway.Upstream{Server: "169.254.169.254:80"}},
		"proxy in the VPC": {Proxy: &gateway.Upstream{Server: "172.31.7.29:8080"}},
		"proxy loopback":   {Proxy: &gateway.Upstream{Server: "127.0.0.1:9301"}},
	}
	for name, s := range bad {
		if err := s.validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
}

func TestTheProxyPasswordNeverLeaves(t *testing.T) {
	s := SessionOptions{Proxy: &gateway.Upstream{Server: "p.example.com:8080", Username: "me", Password: "secret"}}
	pub := s.public()
	if pub.Proxy.Password == "secret" || pub.Proxy.Username != "me" {
		t.Fatalf("public: %+v", pub.Proxy)
	}
	if s.Proxy.Password != "secret" {
		t.Fatalf("masking changed the stored options")
	}
	if (SessionOptions{}).public() != nil {
		t.Fatalf("a session with no options shows options")
	}
}

func TestTimeoutSetsTheLifetime(t *testing.T) {
	if (SessionOptions{}).ttl() != sessionLifetime {
		t.Fatal("default lifetime changed")
	}
	if (SessionOptions{TimeoutMinutes: 45}).ttl() != 45*time.Minute {
		t.Fatal("timeout_minutes ignored")
	}
	if rules := egressTable(netip.MustParseAddr("172.31.7.29")); !strings.Contains(rules, "dnat to 172.31.7.29:3129") || !strings.Contains(rules, "udp dport != 53 drop") {
		t.Fatal("egress rules lost the redirect or the UDP drop")
	}
}
