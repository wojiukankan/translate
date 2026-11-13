package translation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

type memoryCache struct {
	mu     sync.Mutex
	values map[string]string
}

func (c *memoryCache) Get(_ context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[key]
	return v, ok, nil
}

func (c *memoryCache) Put(_ context.Context, key, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
	return nil
}

func newMemoryCache() *memoryCache { return &memoryCache{values: make(map[string]string)} }

func TestTranslateRequestAndCache(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		var body requestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "deepseek-flash" && body.Model != "other-model" {
			t.Errorf("model = %q", body.Model)
		}
		if body.ReasoningEffort != "none" || len(body.Messages) != 2 ||
			body.Messages[0] != (message{Role: "system", Content: "翻译成中文"}) ||
			body.Messages[1] != (message{Role: "user", Content: "  Padding\n文本  "}) {
			t.Errorf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"填充"}}]}`))
	}))
	defer server.Close()

	cache := newMemoryCache()
	svc := NewService(cache, server.Client())
	cfg := Config{BaseURL: server.URL + "/v1", Model: "deepseek-flash", APIKey: "test-key"}
	for i := 0; i < 2; i++ {
		got, err := svc.Translate(context.Background(), cfg, "  Padding\n文本  ")
		if err != nil || got != "填充" {
			t.Fatalf("translation = %q, %v", got, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("same input caused %d requests", calls.Load())
	}
	cfg.Model = "other-model"
	if _, err := svc.Translate(context.Background(), cfg, "  Padding\n文本  "); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("new model caused %d total requests", calls.Load())
	}
	// Recreating the service with the same cache must not make another request.
	svc = NewService(cache, server.Client())
	if _, err := svc.Translate(context.Background(), cfg, "  Padding\n文本  "); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("recreated service caused %d requests", calls.Load())
	}
}

func TestConcurrentTranslationsShareRequest(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}]}`))
	}))
	defer server.Close()
	svc := NewService(newMemoryCache(), server.Client())
	cfg := Config{BaseURL: server.URL, Model: "deepseek-flash"}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			got, err := svc.Translate(context.Background(), cfg, "hello")
			if err != nil || got != "你好" {
				t.Errorf("translation = %q, %v", got, err)
			}
		})
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("got %d API calls", calls.Load())
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"译文"}}]}`))
	}))
	defer server.Close()
	svc := NewService(newMemoryCache(), server.Client())
	cfg := Config{BaseURL: server.URL, Model: "deepseek-flash"}
	if _, err := svc.Translate(context.Background(), cfg, "text"); err == nil {
		t.Fatal("expected HTTP error")
	}
	if got, err := svc.Translate(context.Background(), cfg, "text"); err != nil || got != "译文" {
		t.Fatalf("second translation = %q, %v", got, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("got %d API calls", calls.Load())
	}
}
