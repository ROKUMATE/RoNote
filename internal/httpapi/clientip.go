package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// Proxies are the networks whose X-Forwarded-For the API trusts: Caddy's in
// production, none by default (docs/adr/0014-production-topology.md).
type Proxies []netip.Prefix

func (p Proxies) trusts(a netip.Addr) bool {
	return slices.ContainsFunc(p, func(n netip.Prefix) bool { return n.Contains(a) })
}

// clientIP is the address a request came from. It is the connection's
// peer, unless the peer is a trusted proxy: then X-Forwarded-For is read
// from its end, past each hop a trusted proxy added, to the first that
// isn't one. Anyone can send the header, so what an untrusted peer sends,
// and what a client wrote before its first trusted proxy, never counts.
func (p Proxies) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	client := peer.Unmap()
	if !p.trusts(client) {
		return client.String()
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for _, hop := range slices.Backward(hops) {
		a, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			// A hop no proxy of ours wrote: the last one it did is all
			// that is known.
			break
		}
		client = a.Unmap()
		if !p.trusts(client) {
			break
		}
	}
	return client.String()
}

type clientIPKey struct{}

// withClientIP puts the request's client IP in its context, for the rate
// limits and the access log.
func withClientIP(p Proxies, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, p.clientIP(r))))
	})
}

func clientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}
