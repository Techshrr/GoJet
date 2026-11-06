package links

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// WithLoopbackClientAddress is opt-in at the native entrypoint. The only trusted
// sender is a local ingress that replaces X-Forwarded-For with one literal IP.
// A proxy chain is deliberately not accepted; CDN trust belongs at that ingress.
func WithLoopbackClientAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		peer, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || !peer.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		values := r.Header.Values("X-Forwarded-For")
		if len(values) == 0 {
			// Direct local health probes retain their actual peer identity.
			next.ServeHTTP(w, r)
			return
		}
		client, err := netip.ParseAddr(strings.TrimSpace(values[0]))
		if len(values) != 1 || err != nil || client.Zone() != "" || client.Unmap().IsUnspecified() || client.Unmap().IsMulticast() {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			http.Error(w, "Invalid proxy address", http.StatusBadRequest)
			return
		}
		request := r.Clone(r.Context())
		request.RemoteAddr = net.JoinHostPort(client.Unmap().String(), "0")
		next.ServeHTTP(w, request)
	})
}
