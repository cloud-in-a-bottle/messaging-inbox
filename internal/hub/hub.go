// Package hub keeps up with every app providing the messaging service: which there are, their accounts, and their
// chat lists, which it caches and keeps current from each provider's events. Everything else is fetched from the
// providers when asked for.
package hub

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/cloud-in-a-bottle/messaging-service-spec/messaging"
)

// How often to look for providers that were installed, removed, started or stopped.
const discoverEvery = time.Minute

// Event is a change for the browser, sent as an SSE event named Name.
type Event struct {
	Name string
	Data any
}

// Browser events.
const (
	// The providers or their accounts changed: refetch /api/state.
	EventProviders = "providers"
	// A chat changed (ChatEvent): refetch the list, and its messages if open.
	EventChat = "chat"
	// Someone started or stopped typing (TypingEvent).
	EventTyping = "typing"
)

type ChatEvent struct {
	AppID  string `json:"app_id"`
	ChatID string `json:"chat_id"`
}

type TypingEvent struct {
	AppID    string `json:"app_id"`
	ChatID   string `json:"chat_id"`
	PersonID string `json:"person_id"`
	Typing   bool   `json:"typing"`
}

type Hub struct {
	client *messaging.Client
	log    *slog.Logger

	// Run's context, which the providers are followed under.
	ctx context.Context

	mu      sync.RWMutex
	sources map[string]*source
	subs    map[chan Event]struct{}
}

func New(client *messaging.Client, log *slog.Logger) *Hub {
	return &Hub{client: client, log: log, sources: make(map[string]*source), subs: make(map[chan Event]struct{})}
}

// Client is the messaging service client the hub uses, for calls it doesn't cache.
func (h *Hub) Client() *messaging.Client {
	return h.client
}

// Run looks for providers now and every discoverEvery, and follows them, until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	ticker := time.NewTicker(discoverEvery)
	defer ticker.Stop()
	for {
		if err := h.discover(ctx); err != nil {
			h.log.Error("Failed to list messaging providers", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Rediscover looks for providers now, eg after the owner installed one. ctx bounds only the lookup.
func (h *Hub) Rediscover(ctx context.Context) error {
	return h.discover(ctx)
}

func (h *Hub) discover(ctx context.Context) error {
	h.mu.RLock()
	runCtx := h.ctx
	h.mu.RUnlock()
	if runCtx == nil {
		return errors.New("the hub isn't running")
	}
	infos, err := h.client.Providers(ctx)
	if err != nil {
		return err
	}
	running := make(map[string]messaging.ProviderInfo)
	for _, info := range infos {
		if info.Status == "running" {
			running[info.AppID] = info
		}
	}
	changed := false
	h.mu.Lock()
	for appID, src := range h.sources {
		if _, ok := running[appID]; !ok {
			src.stop()
			delete(h.sources, appID)
			changed = true
		}
	}
	for appID, info := range running {
		if _, ok := h.sources[appID]; !ok {
			src := newSource(h, info)
			h.sources[appID] = src
			go src.run(runCtx)
			changed = true
		}
	}
	h.mu.Unlock()
	if changed {
		h.publish(Event{Name: EventProviders, Data: struct{}{}})
	}
	return nil
}

// Provider is what the hub knows about one provider.
type Provider struct {
	AppID   string             `json:"app_id"`
	AppName string             `json:"app_name"`
	Account *messaging.Account `json:"account"`
	// Why the account or chats couldn't be fetched, if they couldn't.
	Error string `json:"error,omitempty"`
	// Whether its event stream is up, so the cached chats are current.
	Live bool `json:"live"`
}

// Providers lists the providers, by app name.
func (h *Hub) Providers() []Provider {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := []Provider{}
	for _, src := range h.sources {
		out = append(out, src.snapshot())
	}
	slices.SortFunc(out, func(a, b Provider) int { return cmp.Compare(a.AppName, b.AppName) })
	return out
}

// Provider returns the client for one provider, if the hub knows it.
func (h *Hub) Provider(appID string) (*messaging.Provider, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	src, ok := h.sources[appID]
	if !ok {
		return nil, false
	}
	return src.p, true
}

// Chat is a chat with the provider it lives in. (AppID, ID) is unique.
type Chat struct {
	AppID string `json:"app_id"`
	messaging.Chat
}

// Chats lists every provider's cached chats, most recently active first.
func (h *Hub) Chats() []Chat {
	h.mu.RLock()
	out := []Chat{}
	for _, src := range h.sources {
		for _, c := range src.chats {
			out = append(out, Chat{AppID: src.info.AppID, Chat: c})
		}
	}
	h.mu.RUnlock()
	slices.SortStableFunc(out, func(a, b Chat) int {
		if c := cmpTime(b.LastMessageAt, a.LastMessageAt); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.AppID, b.AppID), cmp.Compare(a.ID, b.ID))
	})
	return out
}

func cmpTime(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return a.Compare(*b)
}

// Subscribe returns a channel of browser events. A subscriber that falls behind is dropped: its channel is closed,
// and it should reconnect and refetch.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *Hub) publish(evt Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- evt:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}
