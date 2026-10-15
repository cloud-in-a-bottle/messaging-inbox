// Package server serves the inbox's UI and the JSON API behind it: the hub's cached providers and chats, searches
// across every provider, and everything else passed through to the provider a chat lives in.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	corestatic "github.com/cloud-in-a-bottle/bottled-bridge-core/web/static"

	"github.com/cloud-in-a-bottle/messaging-inbox/internal/hub"
	"github.com/cloud-in-a-bottle/messaging-inbox/internal/static"
)

type Options struct {
	Hub *hub.Hub
	Log *slog.Logger
}

type Server struct {
	hub *hub.Hub
	log *slog.Logger
	mux *http.ServeMux
}

func New(opts Options) *Server {
	s := &Server{hub: opts.Hub, log: opts.Log, mux: http.NewServeMux()}
	mux := s.mux
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, healthResponse{Status: "ok"})
	})
	mux.HandleFunc("GET /{$}", s.index)
	mux.Handle("GET /static/", http.StripPrefix("/static/", noCache(http.FileServerFS(static.FS))))
	// The chat UI's building blocks and look, from the messaging apps' shared core.
	mux.Handle("GET /static/core/", http.StripPrefix("/static/core/", noCache(http.FileServerFS(corestatic.FS))))

	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/rediscover", s.rediscover)
	mux.HandleFunc("GET /api/chats", s.chats)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("/api/p/{app}/{rest...}", s.proxy)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	page, err := fs.ReadFile(static.FS, "index.html")
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(page)
}

// The UI's files change with the app, and are small: revalidate rather than cache.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

type healthResponse struct {
	Status string `json:"status"`
}

type errorBody struct {
	Error  string `json:"error"`
	Detail string `json:"detail"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: code, Detail: detail})
}
