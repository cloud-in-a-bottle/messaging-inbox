package server

import (
	"io"
	"net/http"
	"strings"

	"github.com/cloud-in-a-bottle/messaging-service-spec/messaging"
)

// Response headers passed back from providers: what files need to be shown (or downloaded) safely and cached.
var passedHeaders = []string{
	"Content-Type", "Content-Disposition", "Content-Length", "Cache-Control", "Content-Security-Policy",
	"X-Content-Type-Options", "Last-Modified", "ETag",
}

// proxy passes /api/p/<app ID>/v1/... to that provider's /v1/..., as this app. The browser reaches a chat's
// messages, files, and every action on them this way; the hub's events tell it when to refetch.
func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	if !strings.HasPrefix(rest, "v1/") || rest == "v1/events" {
		writeError(w, http.StatusNotFound, messaging.ErrNotFound, "no such endpoint")
		return
	}
	if _, ok := s.hub.Provider(r.PathValue("app")); !ok {
		writeError(w, http.StatusNotFound, messaging.ErrNotFound, "no such provider")
		return
	}
	c := s.hub.Client()
	target := c.RouterURL + "/api/services/v2/call/" + c.Shortname + "/" + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, messaging.ErrInvalidRequest, err.Error())
		return
	}
	req.ContentLength = r.ContentLength
	for _, h := range []string{"Content-Type", "Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.AppToken)
	req.Header.Set("X-OpenHost-Provider", r.PathValue("app"))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	for _, h := range passedHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
