// Package openrouter is a minimal client for the OpenRouter chat-completions
// API, covering only what the LLM action backend needs: a single multimodal
// (text + one image) request per step with strict json_schema structured
// output. No streaming, tools, or other extras.
package openrouter

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

const defaultBaseURL = "https://openrouter.ai/api/v1"

// requestTimeout bounds a single chat-completion round-trip. Vision + strict
// structured output is slower than a plain text call, so this is generous; the
// runner also passes a context the caller can cancel.
const requestTimeout = 60 * time.Second

// Client talks to the OpenRouter chat-completions endpoint.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

// New builds a Client from the environment. OPENROUTER_API_KEY is required;
// OPENROUTER_BASE_URL overrides the default endpoint (e.g. for tests).
func New() (*Client, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil, errors.New("openrouter: OPENROUTER_API_KEY is not set")
	}
	baseURL := os.Getenv("OPENROUTER_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
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

// JSONSchema is the strict structured-output schema.
type JSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

// Response is the slice of a chat-completions response we read.
type Response struct {
	Choices []Choice `json:"choices"`
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
		return nil, fmt.Errorf("openrouter: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openrouter: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter: status %d: %s", resp.StatusCode, string(responseBody))
	}

	var out Response
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return nil, fmt.Errorf("openrouter: decode response: %w", err)
	}
	return &out, nil
}
