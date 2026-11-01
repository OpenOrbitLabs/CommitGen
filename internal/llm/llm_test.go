package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, tok := range []string{"feat", ": add", " login"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", tok)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, _ := New(Options{BaseURL: srv.URL + "/v1", Timeout: 5 * time.Second})
	got, err := p.Generate(context.Background(), Request{Model: "m"}, nil)
	if err != nil || got != "feat: add login" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestOpenAINonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fix: typo"}}]}`)
	}))
	defer srv.Close()
	p, _ := New(Options{BaseURL: srv.URL, Timeout: 5 * time.Second})
	got, err := p.Generate(context.Background(), Request{}, nil)
	if err != nil || got != "fix: typo" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAnthropicStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.URL.Path != "/v1/messages" {
			t.Errorf("bad request: %s %v", r.URL.Path, r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"docs: \"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"update readme\"}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()
	p, _ := New(Options{Provider: "anthropic", BaseURL: srv.URL, APIKey: "k", Timeout: 5 * time.Second})
	got, err := p.Generate(context.Background(), Request{}, nil)
	if err != nil || got != "docs: update readme" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestHTTPErrorHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, 404)
	}))
	defer srv.Close()
	p, _ := New(Options{BaseURL: srv.URL, Timeout: 5 * time.Second})
	_, err := p.Generate(context.Background(), Request{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
