// Package llmclient is a minimal client for the OpenAI-compatible
// chat-completions API, covering only what the LLM action backend needs: a
// single multimodal (text + one image) request per step with strict
// json_schema structured output. No streaming, tools, or other extras.
//
// The provider comes from the environment: OPENROUTER_API_KEY routes to
// OpenRouter, OPENAI_API_KEY to OpenAI; OpenRouter wins when both are set.
// Both speak the same wire format, so there is no provider-specific code.
package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	openAIBaseURL     = "https://api.openai.com/v1"
)

// requestTimeout bounds a single chat-completion round-trip. Vision + strict
// structured output is slower than a plain text call, so this is generous; the
// runner also passes a context the caller can cancel.
const requestTimeout = 60 * time.Second

// Client talks to an OpenAI-compatible chat-completions endpoint.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

// New builds a Client from the environment. OPENROUTER_API_KEY selects
// OpenRouter, OPENAI_API_KEY selects OpenAI; OpenRouter wins when both are
// set. OPENROUTER_BASE_URL / OPENAI_BASE_URL override the chosen provider's
// endpoint (tests, local OpenAI-compatible servers).
func New() (*Client, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	baseURL := openRouterBaseURL
	override := os.Getenv("OPENROUTER_BASE_URL")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
		baseURL = openAIBaseURL
		override = os.Getenv("OPENAI_BASE_URL")
	}
	if apiKey == "" {
		return nil, errors.New("llmclient: neither OPENROUTER_API_KEY nor OPENAI_API_KEY is set")
	}
	if override != "" {
		baseURL = override
	}
	return &Client{
		httpClient: &http.Client{Timeout: requestTimeout},
		apiKey:     apiKey,
		baseURL:    baseURL,
	}, nil
}

// Request is a chat-completions request body. Only the fields the action
// backend sets are modeled.
type Request struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// Message is one chat message. Content is always the array form (a list of
// parts), which OpenRouter accepts for every role.
type Message struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// ContentPart is one piece of a message: either a text run or an image given
// as a data URL.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL carries an image as a (typically data:) URL.
type ImageURL struct {
	URL string `json:"url"`
}

// TextPart builds a text content part.
func TextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

// ImagePart builds an image content part from a data URL.
func ImagePart(dataURL string) ContentPart {
	return ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: dataURL}}
}

// ResponseFormat pins the model to a strict JSON schema.
type ResponseFormat struct {
	Type       string     `json:"type"`
	JSONSchema JSONSchema `json:"json_schema"`
}

// JSONSchema is the strict structured-output schema. Schema is raw JSON so the
// caller controls property ORDER: OpenAI emits fields in schema order, and a
// reasoning-first schema must not be re-sorted alphabetically (as a Go map
// would be).
type JSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

// Response is the slice of a chat-completions response we read.
type Response struct {
	// Model is the model the provider actually served. A router can satisfy one
	// requested id with a differently-priced variant, so cost accounting reads
	// this rather than the requested id.
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Usage is the provider's token accounting for one call, present on every
// non-streaming OpenAI-compatible chat completion. Zero values mean the
// provider omitted the object.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Choice is one completion choice.
type Choice struct {
	Message ResponseMessage `json:"message"`
}

// ResponseMessage carries the assistant's content. With json_schema output the
// content is a JSON string matching the schema.
type ResponseMessage struct {
	Content string `json:"content"`
}

// ChatCompletion POSTs req to /chat/completions and decodes the response. A
// non-2xx status is returned as an error carrying the response body.
func (c *Client) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("llmclient: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llmclient: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llmclient: request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llmclient: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llmclient: status %d: %s", resp.StatusCode, string(responseBody))
	}

	var out Response
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return nil, fmt.Errorf("llmclient: decode response: %w", err)
	}
	return &out, nil
}
