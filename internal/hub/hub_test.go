package hub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloud-in-a-bottle/messaging-service-spec/messaging"
)

// fakeProvider answers the parts of the messaging service the hub uses, through a stand-in for the router.
type fakeProvider struct {
	mu     sync.Mutex
	chats  map[string]messaging.Chat
	events chan string
}

func (f *fakeProvider) setChat(c messaging.Chat) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chats[c.ID] = c
}

func (f *fakeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/services/v2/call/messaging/")
	switch {
	case path == "v1/account":
		_ = json.NewEncoder(w).Encode(messaging.Account{Network: "fake", NetworkName: "Fake", State: messaging.StateConnected})
	case path == "v1/chats":
		page := messaging.ChatPage{Chats: []messaging.Chat{}}
		for _, c := range f.chats {
			page.Chats = append(page.Chats, c)
		}
		_ = json.NewEncoder(w).Encode(page)
	case strings.HasPrefix(path, "v1/chats/"):
		c, ok := f.chats[strings.TrimPrefix(path, "v1/chats/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(messaging.Error{Code: messaging.ErrNotFound})
			return
		}
		_ = json.NewEncoder(w).Encode(messaging.ChatDetail{Chat: c})
	case path == "v1/events":
		f.mu.Unlock()
		defer f.mu.Lock()
		w.Header().Set("Content-Type", "text/event-stream")
		_ = messaging.WriteEvent(w, messaging.EventHello, struct{}{})
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case chatID := <-f.events:
				_ = messaging.WriteEvent(w, messaging.EventChat, messaging.ChatChanged{ChatID: chatID})
				w.(http.Flusher).Flush()
			}
		}
	default:
		http.NotFound(w, r)
	}
}

func at(minute int) *time.Time {
	t := time.Date(2026, 10, 15, 12, minute, 0, 0, time.UTC)
	return &t
}

func newTestHub(t *testing.T) (*Hub, map[string]*fakeProvider) {
	providers := map[string]*fakeProvider{
		"wa": {chats: map[string]messaging.Chat{"1": {ID: "1", Title: "Alice", LastMessageAt: at(5)}}, events: make(chan string)},
		"fb": {chats: map[string]messaging.Chat{"1": {ID: "1", Title: "Bob", LastMessageAt: at(9)}}, events: make(chan string)},
	}
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/services/v2/providers" {
			_ = json.NewEncoder(w).Encode([]messaging.ProviderInfo{
				{AppID: "wa", AppName: "bottled-whatsapp", Status: "running"},
				{AppID: "fb", AppName: "bottled-fb-messenger", Status: "running"},
				{AppID: "ig", AppName: "bottled-instagram", Status: "stopped"},
			})
			return
		}
		p, ok := providers[r.Header.Get("X-OpenHost-Provider")]
		if !ok {
			http.Error(w, "no provider", http.StatusServiceUnavailable)
			return
		}
		p.ServeHTTP(w, r)
	}))
	t.Cleanup(router.Close)
	client := &messaging.Client{RouterURL: router.URL, AppToken: "token", Shortname: "messaging", HTTP: router.Client()}
	h := New(client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	return h, providers
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func titles(chats []Chat) string {
	var out []string
	for _, c := range chats {
		out = append(out, c.AppID+":"+c.Title)
	}
	return strings.Join(out, ",")
}

func TestHubMergesChatsAndFollowsChanges(t *testing.T) {
	h, providers := newTestHub(t)
	eventually(t, "both providers' chats", func() bool { return len(h.Chats()) == 2 })
	if got := titles(h.Chats()); got != "fb:Bob,wa:Alice" {
		t.Fatalf("unexpected order: %s", got)
	}
	if ps := h.Providers(); len(ps) != 2 || ps[0].AppName != "bottled-fb-messenger" || ps[0].Account == nil {
		t.Fatalf("unexpected providers: %+v", ps)
	}

	events, unsubscribe := h.Subscribe()
	defer unsubscribe()
	providers["wa"].setChat(messaging.Chat{ID: "1", Title: "Alice", LastMessageAt: at(12), UnreadCount: 1})
	providers["wa"].events <- "1"
	eventually(t, "the changed chat to move up", func() bool { return titles(h.Chats()) == "wa:Alice,fb:Bob" })
	for evt := range events {
		if c, ok := evt.Data.(ChatEvent); ok && evt.Name == EventChat && c.AppID == "wa" && c.ChatID == "1" {
			break
		}
	}

	providers["fb"].mu.Lock()
	delete(providers["fb"].chats, "1")
	providers["fb"].mu.Unlock()
	providers["fb"].events <- "1"
	eventually(t, "the deleted chat to go", func() bool { return titles(h.Chats()) == "wa:Alice" })
}
