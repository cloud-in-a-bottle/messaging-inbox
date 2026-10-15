package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cloud-in-a-bottle/messaging-service-spec/messaging"
)

const (
	// The most chats kept per provider: the list shows recent activity, and older chats are a search away.
	maxChats  = 1000
	chatsPage = 250
	// Changes to a chat often come in bursts (a backfilled page, a message and its receipts); they're refetched once
	// this long after the first.
	coalesce = 250 * time.Millisecond
	// The longest wait between attempts to reconnect to a provider's events.
	maxBackoff = 30 * time.Second
)

// source follows one provider. Its cached fields are guarded by the hub's mutex.
type source struct {
	h      *Hub
	info   messaging.ProviderInfo
	p      *messaging.Provider
	cancel context.CancelFunc
	done   chan struct{}

	account *messaging.Account
	err     string
	live    bool
	chats   map[string]messaging.Chat

	dirtyMu sync.Mutex
	dirty   map[string]bool
}

func newSource(h *Hub, info messaging.ProviderInfo) *source {
	return &source{
		h: h, info: info, p: h.client.Provider(info.AppID), done: make(chan struct{}),
		chats: make(map[string]messaging.Chat), dirty: make(map[string]bool),
	}
}

func (s *source) snapshot() Provider {
	return Provider{AppID: s.info.AppID, AppName: s.info.AppName, Account: s.account, Error: s.err, Live: s.live}
}

func (s *source) stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// run follows the provider's events until ctx ends or the source is stopped, reconnecting when the stream drops.
// Each connection starts with "hello", on which everything cached is refetched.
func (s *source) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.h.mu.Lock()
	s.cancel = cancel
	s.h.mu.Unlock()
	defer cancel()
	log := s.h.log.With("provider", s.info.AppName)
	backoff := time.Second
	for ctx.Err() == nil {
		stream, err := s.p.Events(ctx)
		if err != nil {
			log.Warn("Can't follow the provider's events", "err", err)
			s.setError(err)
		} else {
			for evt := range stream {
				backoff = time.Second
				s.handle(ctx, evt)
			}
			s.setLive(false)
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (s *source) handle(ctx context.Context, evt messaging.StreamEvent) {
	switch evt.Name {
	case messaging.EventHello:
		s.refreshAccount(ctx)
		s.refreshChats(ctx)
		s.setLive(true)
	case messaging.EventChats:
		s.refreshChats(ctx)
	case messaging.EventAccount:
		s.refreshAccount(ctx)
	case messaging.EventChat:
		var c messaging.ChatChanged
		if json.Unmarshal(evt.Data, &c) == nil {
			s.markDirty(ctx, c.ChatID)
		}
	case messaging.EventTyping:
		var t messaging.TypingChanged
		if json.Unmarshal(evt.Data, &t) == nil {
			s.h.publish(Event{Name: EventTyping, Data: TypingEvent{
				AppID: s.info.AppID, ChatID: t.ChatID, PersonID: t.PersonID, Typing: t.Typing,
			}})
		}
	}
}

func (s *source) setError(err error) {
	s.h.mu.Lock()
	s.err = err.Error()
	s.h.mu.Unlock()
	s.h.publish(Event{Name: EventProviders, Data: struct{}{}})
}

func (s *source) setLive(live bool) {
	s.h.mu.Lock()
	s.live = live
	s.h.mu.Unlock()
	s.h.publish(Event{Name: EventProviders, Data: struct{}{}})
}

func (s *source) refreshAccount(ctx context.Context) {
	account, err := s.p.Account(ctx)
	if err != nil {
		s.setError(err)
		return
	}
	s.h.mu.Lock()
	s.account = account
	s.err = ""
	s.h.mu.Unlock()
	s.h.publish(Event{Name: EventProviders, Data: struct{}{}})
}

func (s *source) refreshChats(ctx context.Context) {
	chats := make(map[string]messaging.Chat)
	cursor := ""
	for len(chats) < maxChats {
		page, err := s.p.Chats(ctx, messaging.ChatsQuery{Limit: chatsPage, Cursor: cursor})
		if err != nil {
			s.setError(err)
			return
		}
		for _, c := range page.Chats {
			chats[c.ID] = c
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	s.h.mu.Lock()
	s.chats = chats
	s.h.mu.Unlock()
	s.h.publish(Event{Name: EventChat, Data: ChatEvent{AppID: s.info.AppID}})
}

// markDirty refetches a chat shortly, along with any others that change meanwhile.
func (s *source) markDirty(ctx context.Context, chatID string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	if len(s.dirty) == 0 {
		time.AfterFunc(coalesce, func() { s.flushDirty(ctx) })
	}
	s.dirty[chatID] = true
}

func (s *source) flushDirty(ctx context.Context) {
	s.dirtyMu.Lock()
	ids := s.dirty
	s.dirty = make(map[string]bool)
	s.dirtyMu.Unlock()
	for id := range ids {
		detail, err := s.p.Chat(ctx, id)
		var apiErr *messaging.Error
		switch {
		case err == nil:
			s.h.mu.Lock()
			if detail.Archived {
				delete(s.chats, id)
			} else {
				s.chats[id] = detail.Chat
			}
			s.h.mu.Unlock()
		case errors.As(err, &apiErr) && apiErr.Code == messaging.ErrNotFound:
			s.h.mu.Lock()
			delete(s.chats, id)
			s.h.mu.Unlock()
		default:
			if ctx.Err() == nil {
				s.h.log.Warn("Failed to refetch a chat", "provider", s.info.AppName, "chat", id, "err", err)
			}
			continue
		}
		s.h.publish(Event{Name: EventChat, Data: ChatEvent{AppID: s.info.AppID, ChatID: id}})
	}
}
