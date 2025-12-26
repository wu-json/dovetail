package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
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

// mockLocalClient implements LocalClient for testing
type mockLocalClient struct {
	whoisResponse *apitype.WhoIsResponse
	whoisErr      error
}

func (m *mockLocalClient) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	if m.whoisErr != nil {
		return nil, m.whoisErr
	}
	return m.whoisResponse, nil
}

func TestInjectIdentity_NilLocalClient(t *testing.T) {
	targetURL, _ := url.Parse("http://localhost:8080")
	logger := slog.Default()

	p := New(targetURL, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	p.injectIdentity(req)

	// Should not set any headers
	if req.Header.Get(HeaderUser) != "" {
		t.Error("expected no HeaderUser when localClient is nil")
	}
}

func TestInjectIdentity_WhoIsError(t *testing.T) {
	targetURL, _ := url.Parse("http://localhost:8080")
	logger := slog.Default()

	mock := &mockLocalClient{
		whoisErr: errors.New("whois lookup failed"),
	}
	p := New(targetURL, mock, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "100.100.100.1:12345"
	p.injectIdentity(req)

	// Should not set any headers on error
	if req.Header.Get(HeaderUser) != "" {
		t.Error("expected no HeaderUser on WhoIs error")
	}
}

func TestInjectIdentity_WithUserProfile(t *testing.T) {
	targetURL, _ := url.Parse("http://localhost:8080")
	logger := slog.Default()

	mock := &mockLocalClient{
		whoisResponse: &apitype.WhoIsResponse{
			UserProfile: &tailcfg.UserProfile{
				LoginName:   "user@example.com",
				DisplayName: "Test User",
			},
		},
	}
	p := New(targetURL, mock, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "100.100.100.1:12345"
	p.injectIdentity(req)

	if got := req.Header.Get(HeaderUser); got != "user@example.com" {
		t.Errorf("HeaderUser = %q, want %q", got, "user@example.com")
	}
	if got := req.Header.Get(HeaderName); got != "Test User" {
		t.Errorf("HeaderName = %q, want %q", got, "Test User")
	}
}

func TestInjectIdentity_WithNode(t *testing.T) {
	targetURL, _ := url.Parse("http://localhost:8080")
	logger := slog.Default()

	hostinfo := &tailcfg.Hostinfo{
		Hostname: "test-machine",
	}

	mock := &mockLocalClient{
		whoisResponse: &apitype.WhoIsResponse{
			Node: &tailcfg.Node{
				ComputedName: "test-node",
				Hostinfo:     hostinfo.View(),
			},
		},
	}
	p := New(targetURL, mock, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "100.100.100.1:12345"
	p.injectIdentity(req)

	if got := req.Header.Get(HeaderLogin); got != "test-node" {
		t.Errorf("HeaderLogin = %q, want %q", got, "test-node")
	}
}

func TestInjectIdentity_FullResponse(t *testing.T) {
	targetURL, _ := url.Parse("http://localhost:8080")
	logger := slog.Default()

	hostinfo := &tailcfg.Hostinfo{
		Hostname: "my-laptop",
	}

	mock := &mockLocalClient{
		whoisResponse: &apitype.WhoIsResponse{
			UserProfile: &tailcfg.UserProfile{
				LoginName:   "alice@example.com",
				DisplayName: "Alice Smith",
			},
			Node: &tailcfg.Node{
				ComputedName: "alice-laptop",
				Hostinfo:     hostinfo.View(),
			},
		},
	}
	p := New(targetURL, mock, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "100.100.100.1:12345"
	p.injectIdentity(req)

	tests := []struct {
		header string
		want   string
	}{
		{HeaderUser, "alice@example.com"},
		{HeaderName, "Alice Smith"},
		{HeaderLogin, "alice-laptop"},
	}

	for _, tt := range tests {
		if got := req.Header.Get(tt.header); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestServeHTTP(t *testing.T) {
	// Create a mock backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back some request info
		w.Header().Set("X-Received-Host", r.Host)
		w.Header().Set("X-Received-User", r.Header.Get(HeaderUser))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)
	logger := slog.Default()

	mock := &mockLocalClient{
		whoisResponse: &apitype.WhoIsResponse{
			UserProfile: &tailcfg.UserProfile{
				LoginName:   "test@example.com",
				DisplayName: "Test User",
			},
		},
	}
	p := New(backendURL, mock, logger)

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "https://proxy.example.com/test", nil)
	req.RemoteAddr = "100.100.100.1:12345"
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("Body = %q, want %q", string(body), "OK")
	}

	// Verify the backend received the identity header
	if got := resp.Header.Get("X-Received-User"); got != "test@example.com" {
		t.Errorf("Backend received HeaderUser = %q, want %q", got, "test@example.com")
	}
}
