package server

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloud-in-a-bottle/messaging-service-spec/messaging"

	"github.com/cloud-in-a-bottle/messaging-inbox/internal/hub"
)

type providerView struct {
	hub.Provider
	// The provider's own UI, where its account is logged in and set up.
	UIURL string `json:"ui_url"`
}

type stateResponse struct {
	Providers []providerView `json:"providers"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	out := stateResponse{Providers: []providerView{}}
	for _, p := range s.hub.Providers() {
		out.Providers = append(out.Providers, providerView{Provider: p, UIURL: siblingURL(r, p.AppName)})
	}
	writeJSON(w, out)
}

// siblingURL is another app's address on the domain this request came in on: apps are subdomains of it, so the
// inbox's own label is swapped for the app's name. The router sets Host to the public name and X-Forwarded-Proto.
func siblingURL(r *http.Request, appName string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "https"
	}
	_, domain, _ := strings.Cut(r.Host, ".")
	return fmt.Sprintf("%s://%s.%s/", scheme, appName, domain)
}

func (s *Server) rediscover(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.Rediscover(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "discovery_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) chats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.hub.Chats())
}

type searchHit struct {
	AppID string `json:"app_id"`
	messaging.SearchHit
}

type searchResponse struct {
	Hits []searchHit `json:"hits"`
	// Providers whose search failed, by app ID, with why.
	Failed map[string]string `json:"failed"`
}

// search asks every provider and merges their hits, newest first.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, messaging.ErrInvalidRequest, "q is required")
		return
	}
	out := searchResponse{Hits: []searchHit{}, Failed: map[string]string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range s.hub.Providers() {
		provider, ok := s.hub.Provider(p.AppID)
		if !ok {
			continue
		}
		wg.Go(func() {
			res, err := provider.Search(r.Context(), q, "", 50)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Failed[p.AppID] = err.Error()
				return
			}
			for _, h := range res.Hits {
				out.Hits = append(out.Hits, searchHit{AppID: p.AppID, SearchHit: h})
			}
		})
	}
	wg.Wait()
	slices.SortStableFunc(out.Hits, func(a, b searchHit) int {
		return cmp.Or(b.Message.SentAt.Compare(a.Message.SentAt), cmp.Compare(a.AppID, b.AppID))
	})
	if len(out.Hits) > 100 {
		out.Hits = out.Hits[:100]
	}
	writeJSON(w, out)
}

// events streams the hub's events to the browser. When the hub drops a subscriber that fell behind, the stream ends
// and the browser's EventSource reconnects and refetches on "hello".
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()
	if err := messaging.WriteEvent(w, messaging.EventHello, struct{}{}); err != nil {
		return
	}
	flusher.Flush()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
		case evt, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(evt.Data)
			if err != nil {
				panic(err)
			}
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Name, data); err != nil {
				return
			}
		}
		flusher.Flush()
	}
}
