package server

import (
	"net/http/httptest"
	"testing"
)

func TestSiblingURL(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "messaging-inbox.home.example.com"
	if got := siblingURL(r, "bottled-whatsapp"); got != "https://bottled-whatsapp.home.example.com/" {
		t.Fatalf("got %s", got)
	}
	r.Host = "messaging-inbox.box.local:8080"
	r.Header.Set("X-Forwarded-Proto", "http")
	if got := siblingURL(r, "bottled-whatsapp"); got != "http://bottled-whatsapp.box.local:8080/" {
		t.Fatalf("got %s", got)
	}
}
