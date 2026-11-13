package translation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

const systemPrompt = "翻译成中文"
const reasoningEffort = "none"

// Config is the Chat Completions endpoint and its credentials.
type Config struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"-"`
}

// Cache holds completed translations keyed by their exact input and settings.
type Cache interface {
	Get(context.Context, string) (string, bool, error)
	Put(context.Context, string, string) error
}

type call struct {
	done   chan struct{}
	result string
	err    error
}

// Service shares concurrent requests for the same input and stores results.
type Service struct {
	cache   Cache
	client  *http.Client
	mu      sync.Mutex
	pending map[string]*call
}

func NewService(cache Cache, client *http.Client) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	return &Service{cache: cache, client: client, pending: make(map[string]*call)}
}

func cacheKey(cfg Config, text string) string {
	b, _ := json.Marshal([]string{text, cfg.BaseURL, cfg.Model, systemPrompt, reasoningEffort})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Translate returns a cached result or calls the configured API once.
func (s *Service) Translate(ctx context.Context, cfg Config, text string) (string, error) {
	key := cacheKey(cfg, text)
	s.mu.Lock()
	if existing := s.pending[key]; existing != nil {
		s.mu.Unlock()
		select {
		case <-existing.done:
			return existing.result, existing.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	current := &call{done: make(chan struct{})}
	s.pending[key] = current
	s.mu.Unlock()

	current.result, current.err = s.translate(ctx, cfg, text, key)
	s.mu.Lock()
	delete(s.pending, key)
	close(current.done)
	s.mu.Unlock()
	return current.result, current.err
}

func (s *Service) translate(ctx context.Context, cfg Config, text, key string) (string, error) {
	cached, found, err := s.cache.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("读取翻译缓存失败：%w", err)
	}
	if found {
		return cached, nil
	}
	result, err := s.request(ctx, cfg, text)
	if err != nil {
		return "", err
	}
	if err := s.cache.Put(ctx, key, result); err != nil {
		return "", fmt.Errorf("保存翻译缓存失败：%w", err)
	}
	return result, nil
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type requestBody struct {
	Model           string    `json:"model"`
	Messages        []message `json:"messages"`
	ReasoningEffort string    `json:"reasoning_effort"`
}

func (s *Service) request(ctx context.Context, cfg Config, text string) (string, error) {
	body, err := json.Marshal(requestBody{
		Model: cfg.Model,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
		ReasoningEffort: reasoningEffort,
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("接口地址无效：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求翻译接口失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("翻译接口返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("读取翻译接口响应失败：%w", err)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == nil {
		return "", errors.New("翻译接口没有返回译文")
	}
	return *result.Choices[0].Message.Content, nil
}
