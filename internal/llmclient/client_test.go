package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCompletionRequestShapeAndParse(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"reasoning\":\"tap login\",\"ranked\":[2,0]}"}}]}`))
	}))
	defer server.Close()

	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("OPENROUTER_BASE_URL", server.URL)
	client, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.ChatCompletion(context.Background(), Request{
		Model: "vendor/model",
		Messages: []Message{
			{Role: "system", Content: []ContentPart{TextPart("system")}},
			{Role: "user", Content: []ContentPart{
				TextPart("candidates"),
				ImagePart("data:image/png;base64,AAAA"),
			}},
		},
		ResponseFormat: &ResponseFormat{
			Type: "json_schema",
			JSONSchema: JSONSchema{
				Name:   "ranked_actions",
				Strict: true,
				Schema: json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	// Request carried the model.
	if captured["model"] != "vendor/model" {
		t.Errorf("model = %v, want vendor/model", captured["model"])
	}
	// Request carried an image_url content part.
	messages := captured["messages"].([]any)
	user := messages[1].(map[string]any)
	parts := user["content"].([]any)
	foundImage := false
	for _, part := range parts {
		if part.(map[string]any)["type"] == "image_url" {
			foundImage = true
			image := part.(map[string]any)["image_url"].(map[string]any)
			if !strings.HasPrefix(image["url"].(string), "data:image/png;base64,") {
				t.Errorf("image url = %v, want data URL", image["url"])
			}
		}
	}
	if !foundImage {
		t.Error("request carried no image_url content part")
	}
	// Request carried the strict json_schema response_format.
	rf := captured["response_format"].(map[string]any)
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", rf["type"])
	}
	if schema := rf["json_schema"].(map[string]any); schema["strict"] != true {
		t.Errorf("json_schema.strict = %v, want true", schema["strict"])
	}

	// Response parsed into the ranked-index content.
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	var content struct {
		Reasoning string `json:"reasoning"`
		Ranked    []int  `json:"ranked"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content.Reasoning != "tap login" {
		t.Errorf("reasoning = %q, want tap login", content.Reasoning)
	}
	if len(content.Ranked) != 2 || content.Ranked[0] != 2 || content.Ranked[1] != 0 {
		t.Errorf("ranked = %v, want [2 0]", content.Ranked)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := New(); err == nil {
		t.Fatal("expected error when neither API key is set")
	}
}

func TestNewFallsBackToOpenAIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer openai-key" {
			t.Errorf("Authorization = %q, want Bearer openai-key", got)
		}
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("OPENAI_BASE_URL", server.URL)
	client, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.ChatCompletion(context.Background(), Request{Model: "m"}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
}

func TestNewPrefersOpenRouterOverOpenAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer router-key" {
			t.Errorf("Authorization = %q, want Bearer router-key (OpenRouter must win)", got)
		}
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	t.Setenv("OPENROUTER_API_KEY", "router-key")
	t.Setenv("OPENROUTER_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1") // unreachable; must not be used
	client, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.ChatCompletion(context.Background(), Request{Model: "m"}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
}

func TestChatCompletionSurfacesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("OPENROUTER_BASE_URL", server.URL)
	client, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.ChatCompletion(context.Background(), Request{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error, got %v", err)
	}
}
