package pairing

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestLink(t *testing.T) {
	tests := []struct {
		name    string
		params  Params
		want    string
		wantErr string
	}{
		{
			name:   "lework scheme keeps percent encoding",
			params: Params{PublicURL: "http://dev.ckeyer.com", Token: "abc-123"},
			want:   "lework://connect?url=http%3A%2F%2Fdev.ckeyer.com&token=abc-123",
		},
		{
			name:   "trailing slash is trimmed from public url",
			params: Params{PublicURL: "http://dev.ckeyer.com/", Token: "abc-123"},
			want:   "lework://connect?url=http%3A%2F%2Fdev.ckeyer.com&token=abc-123",
		},
		{
			name:   "expo url switches to exp scheme",
			params: Params{PublicURL: "http://dev.ckeyer.com", Token: "abc-123", ExpoURL: "http://dev.ckeyer.com:8082"},
			want:   "exp://dev.ckeyer.com:8082/--/connect?url=http%3A%2F%2Fdev.ckeyer.com&token=abc-123",
		},
		{
			name:   "expo url with https uses exps scheme",
			params: Params{PublicURL: "https://cp.example.com", Token: "abc-123", ExpoURL: "https://dev.example.com:8082"},
			want:   "exps://dev.example.com:8082/--/connect?url=https%3A%2F%2Fcp.example.com&token=abc-123",
		},
		{
			name:   "expo url drops subpath",
			params: Params{PublicURL: "https://cp.example.com", Token: "abc-123", ExpoURL: "https://dev.example.com/metro/"},
			want:   "exps://dev.example.com/--/connect?url=https%3A%2F%2Fcp.example.com&token=abc-123",
		},
		{
			name:    "token is required",
			params:  Params{PublicURL: "http://dev.ckeyer.com", Token: "   "},
			wantErr: "token is required",
		},
		{
			name:    "public url is required",
			params:  Params{PublicURL: "  ", Token: "abc-123"},
			wantErr: "public URL is required",
		},
		{
			name:    "public url needs a scheme",
			params:  Params{PublicURL: "dev.ckeyer.com", Token: "abc-123"},
			wantErr: "must start with http:// or https://",
		},
		{
			name:    "expo url must be http",
			params:  Params{PublicURL: "http://dev.ckeyer.com", Token: "abc-123", ExpoURL: "dev.ckeyer.com:8082"},
			wantErr: "expo URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Link(tt.params)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Link() = %q, want error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Link() error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Link() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Link() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLinkRoundTrip(t *testing.T) {
	link, err := Link(Params{PublicURL: "http://dev.ckeyer.com", Token: "token with space/slash"})
	if err != nil {
		t.Fatalf("Link() error = %v", err)
	}
	query := link[strings.Index(link, "?")+1:]
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", query, err)
	}
	if got := values.Get("url"); got != "http://dev.ckeyer.com" {
		t.Fatalf("url = %q", got)
	}
	if got := values.Get("token"); got != "token with space/slash" {
		t.Fatalf("token = %q", got)
	}
}

func TestNormalizePublicURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "plain host", raw: "http://dev.ckeyer.com", want: "http://dev.ckeyer.com"},
		{name: "keeps subpath", raw: "https://example.com/lework/", want: "https://example.com/lework"},
		{name: "drops query and fragment", raw: "http://box:8787/?a=1#b", want: "http://box:8787"},
		{name: "empty", raw: " ", wantErr: "public URL is required"},
		{name: "no scheme", raw: "box:8787", wantErr: "must start with http:// or https://"},
		{name: "no host", raw: "http://", wantErr: "public URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePublicURL(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NormalizePublicURL(%q) error = %v, want containing %q", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePublicURL(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePublicURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestResolvePublicURL(t *testing.T) {
	t.Run("explicit url wins", func(t *testing.T) {
		got, err := ResolvePublicURL("127.0.0.1:8787", "http://dev.ckeyer.com/")
		if err != nil {
			t.Fatalf("ResolvePublicURL() error = %v", err)
		}
		if got != "http://dev.ckeyer.com" {
			t.Fatalf("ResolvePublicURL() = %q", got)
		}
	})

	t.Run("falls back to lan ip and listen port", func(t *testing.T) {
		got, err := ResolvePublicURL("127.0.0.1:8787", "")
		if err != nil {
			t.Skipf("no LAN IPv4 on this machine: %v", err)
		}
		pattern := regexp.MustCompile(`^http://[0-9.]+:8787$`)
		if !pattern.MatchString(got) {
			t.Fatalf("ResolvePublicURL() = %q, want http://<lan-ip>:8787", got)
		}
	})

	t.Run("invalid listen address", func(t *testing.T) {
		if _, err := ResolvePublicURL("127.0.0.1", ""); err == nil {
			t.Fatal("ResolvePublicURL() error = nil, want error")
		}
	})
}
