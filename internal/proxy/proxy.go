package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"

	"tailscale.com/client/tailscale"
)

const (
	HeaderUser    = "X-Tailscale-User"
	HeaderName    = "X-Tailscale-Name"
	HeaderLogin   = "X-Tailscale-Login"
	HeaderTailnet = "X-Tailscale-Tailnet"
)

type Proxy struct {
	target      atomic.Pointer[url.URL]
	localClient *tailscale.LocalClient
	logger      *slog.Logger
	handler     http.Handler
}

func New(targetURL *url.URL, localClient *tailscale.LocalClient, logger *slog.Logger) *Proxy {
	p := &Proxy{
		localClient: localClient,
		logger:      logger,
	}
	p.target.Store(targetURL)

	rp := &httputil.ReverseProxy{
		Director: p.director,
	}

	p.handler = rp
	return p
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.handler.ServeHTTP(w, r)
}

func (p *Proxy) UpdateTarget(target *url.URL) {
	p.target.Store(target)
}

func (p *Proxy) director(req *http.Request) {
	target := p.target.Load()
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.Host = target.Host

	// Inject Tailscale identity headers
	p.injectIdentity(req)
}

func (p *Proxy) injectIdentity(req *http.Request) {
	if p.localClient == nil {
		return
	}

	whois, err := p.localClient.WhoIs(req.Context(), req.RemoteAddr)
	if err != nil {
		p.logger.Debug("failed to get whois info", "remote", req.RemoteAddr, "error", err)
		return
	}

	if whois.UserProfile != nil {
		req.Header.Set(HeaderUser, whois.UserProfile.LoginName)
		req.Header.Set(HeaderName, whois.UserProfile.DisplayName)
	}

	if whois.Node != nil {
		req.Header.Set(HeaderLogin, whois.Node.ComputedName)
		if whois.Node.Hostinfo.Valid() {
			req.Header.Set(HeaderTailnet, string(whois.Node.Hostinfo.Hostname()))
		}
	}
}
