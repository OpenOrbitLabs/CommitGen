package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Request struct {
	System      string
	User        string
	Model       string
	Temperature float64
	MaxTokens   int
}

type OnToken func(string)

type Provider interface {
	Generate(ctx context.Context, r Request, on OnToken) (string, error)
}

type Options struct {
	Provider string
	BaseURL  string
	APIKey   string
	Timeout  time.Duration
	NumCtx   int 
}

func IsOllama(base string) bool { return strings.Contains(base, ":11434") }

func New(o Options) (Provider, error) {
	client := &http.Client{Timeout: o.Timeout}
	switch strings.ToLower(o.Provider) {
	case "ollama":
		return newOllama(o, client), nil
	case "", "openai", "openai-compatible":
		if IsOllama(o.BaseURL) {
			return newOllama(o, client), nil
		}
		return &openAI{base: strings.TrimRight(o.BaseURL, "/"), key: o.APIKey, http: client}, nil
	case "anthropic", "claude":
		base := strings.TrimRight(o.BaseURL, "/")
		if base == "" || strings.Contains(base, "localhost:11434") {
			base = "https://api.anthropic.com"
		}
		if o.APIKey == "" {
			return nil, fmt.Errorf("anthropic provider needs an API key (set ANTHROPIC_API_KEY)")
		}
		return &anthropic{base: base, key: o.APIKey, http: client}, nil
	}
	return nil, fmt.Errorf("unknown provider %q (use \"openai\" or \"anthropic\")", o.Provider)
}

func httpError(resp *http.Response, base string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
	msg := strings.TrimSpace(string(b))
	hint := ""
	switch resp.StatusCode {
	case 401, 403:
		hint = " (check your API key)"
	case 404:
		hint = " (check base_url and that the model exists — for Ollama run `ollama pull <model>`)"
	}
	return fmt.Errorf("%s returned HTTP %d%s: %s", base, resp.StatusCode, hint, msg)
}

func connError(err error, base string) error {
	if strings.Contains(base, "localhost") || strings.Contains(base, "127.0.0.1") {
		return fmt.Errorf("could not reach %s — is your local model server running? (e.g. `ollama serve`): %w", base, err)
	}
	return fmt.Errorf("request to %s failed: %w", base, err)
}

type openAI struct {
	base, key string
	http      *http.Client
}

func (p *openAI) Generate(ctx context.Context, r Request, on OnToken) (string, error) {
	payload := map[string]any{
		"model":       r.Model,
		"temperature": r.Temperature,
		"max_tokens":  r.MaxTokens,
		"stream":      true,
		"messages": []map[string]string{
			{"role": "system", "content": r.System},
			{"role": "user", "content": r.User},
		},
	}

	if strings.Contains(strings.ToLower(r.Model), "gpt-oss") {
		payload["reasoning_effort"] = "low"
		if r.MaxTokens < 2048 {
			payload["max_tokens"] = 2048
		}
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", p.base+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if p.key != "" {
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return "", connError(err, p.base)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", httpError(resp, p.base)
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		var out struct {
			Choices []struct {
				Message struct{ Content string } `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
			return "", fmt.Errorf("unexpected response from %s", p.base)
		}
		if on != nil {
			on(out.Choices[0].Message.Content)
		}
		return out.Choices[0].Message.Content, nil
	}

	var full strings.Builder
	err = readSSE(resp.Body, func(data string) bool {
		if data == "[DONE]" {
			return false
		}
		var chunk struct {
			Choices []struct {
				Delta struct{ Content string } `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) == nil && len(chunk.Choices) > 0 {
			t := chunk.Choices[0].Delta.Content
			full.WriteString(t)
			if on != nil && t != "" {
				on(t)
			}
		}
		return true
	})
	return full.String(), err
}

type ollama struct {
	base   string
	numCtx int
	http   *http.Client
}

func newOllama(o Options, c *http.Client) *ollama {
	base := strings.TrimRight(o.BaseURL, "/")
	base = strings.TrimSuffix(base, "/v1")
	if base == "" {
		base = "http://localhost:11434"
	}
	n := o.NumCtx
	if n <= 0 {
		n = 8192
	}
	return &ollama{base: base, numCtx: n, http: c}
}

func (p *ollama) Generate(ctx context.Context, r Request, on OnToken) (string, error) {
	maxTok := r.MaxTokens
	if maxTok == 0 {
		maxTok = 400
	}
	body, _ := json.Marshal(map[string]any{
		"model":  r.Model,
		"stream": true,
		"options": map[string]any{
			"temperature": r.Temperature,
			"num_ctx":     p.numCtx,
			"num_predict": maxTok,
		},
		"messages": []map[string]string{
			{"role": "system", "content": r.System},
			{"role": "user", "content": r.User},
		},
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", p.base+"/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return "", connError(err, p.base)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", httpError(resp, p.base)
	}
	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var chunk struct {
			Message struct{ Content string } `json:"message"`
			Done    bool                     `json:"done"`
			Error   string                   `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &chunk) != nil {
			continue
		}
		if chunk.Error != "" {
			return "", fmt.Errorf("ollama: %s", chunk.Error)
		}
		full.WriteString(chunk.Message.Content)
		if on != nil && chunk.Message.Content != "" {
			on(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	return full.String(), sc.Err()
}

type anthropic struct {
	base, key string
	http      *http.Client
}

func (p *anthropic) Generate(ctx context.Context, r Request, on OnToken) (string, error) {
	maxTok := r.MaxTokens
	if maxTok == 0 {
		maxTok = 400
	}
	body, _ := json.Marshal(map[string]any{
		"model":       r.Model,
		"max_tokens":  maxTok,
		"temperature": r.Temperature,
		"stream":      true,
		"system":      r.System,
		"messages":    []map[string]string{{"role": "user", "content": r.User}},
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", p.base+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.http.Do(req)
	if err != nil {
		return "", connError(err, p.base)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", httpError(resp, p.base)
	}
	var full strings.Builder
	var streamErr error
	err = readSSE(resp.Body, func(data string) bool {
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return true
		}
		switch ev.Type {
		case "content_block_delta":
			full.WriteString(ev.Delta.Text)
			if on != nil && ev.Delta.Text != "" {
				on(ev.Delta.Text)
			}
		case "message_stop":
			return false
		case "error":
			streamErr = fmt.Errorf("anthropic: %s", ev.Error.Message)
			return false
		}
		return true
	})
	if streamErr != nil {
		return "", streamErr
	}
	return full.String(), err
}

func readSSE(r io.Reader, fn func(string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if !fn(strings.TrimSpace(strings.TrimPrefix(line, "data:"))) {
			return nil
		}
	}
	return sc.Err()
}
