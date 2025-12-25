package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestNew(t *testing.T) {
	targetURL, _ := url.Parse("http://localhost:8080")
	logger := slog.Default()

	p := New(targetURL, nil, logger)

	if p == nil {
		t.Fatal("expected non-nil proxy")
	}

	if p.target.Load().String() != targetURL.String() {
		t.Errorf("target = %q, want %q", p.target.Load().String(), targetURL.String())
	}
}

func TestUpdateTarget(t *testing.T) {
	initialURL, _ := url.Parse("http://localhost:8080")
	newURL, _ := url.Parse("http://localhost:9090")
	logger := slog.Default()

	p := New(initialURL, nil, logger)
	p.UpdateTarget(newURL)

	if p.target.Load().String() != newURL.String() {
		t.Errorf("target = %q, want %q", p.target.Load().String(), newURL.String())
	}
}

func TestDirector(t *testing.T) {
	targetURL, _ := url.Parse("http://backend:8080")
	logger := slog.Default()

	p := New(targetURL, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "https://original.example.com/path?query=1", nil)

	p.director(req)

	if req.URL.Scheme != "http" {
		t.Errorf("scheme = %q, want %q", req.URL.Scheme, "http")
	}

	if req.URL.Host != "backend:8080" {
		t.Errorf("host = %q, want %q", req.URL.Host, "backend:8080")
	}

	if req.Host != "backend:8080" {
		t.Errorf("req.Host = %q, want %q", req.Host, "backend:8080")
	}

	if req.URL.Path != "/path" {
		t.Errorf("path = %q, want %q", req.URL.Path, "/path")
	}

	if req.URL.RawQuery != "query=1" {
		t.Errorf("query = %q, want %q", req.URL.RawQuery, "query=1")
	}
}

func TestHeaderConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"HeaderUser", HeaderUser, "X-Tailscale-User"},
		{"HeaderName", HeaderName, "X-Tailscale-Name"},
		{"HeaderLogin", HeaderLogin, "X-Tailscale-Login"},
		{"HeaderTailnet", HeaderTailnet, "X-Tailscale-Tailnet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}
