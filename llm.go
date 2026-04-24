package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LLMClient handles communication with an OpenAI-compatible LLM API
type LLMClient struct {
	BaseURL    string
	APIKey     string
	httpClient *http.Client
}

// NewLLMClient creates a new LLM client
func NewLLMClient(baseURL, apiKey string) *LLMClient {
	return &LLMClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// --- Wire types for OpenAI-compatible Chat Completion ---

// ChatMessage is a single turn in a conversation.
// Content is typed as any so we can store either a string or a []contentPart.
type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string | []contentPart
}

// TextMessage returns a plain-text ChatMessage
func TextMessage(role, text string) ChatMessage {
	return ChatMessage{Role: role, Content: text}
}

// MultiImageMessage returns a user message containing multiple page images and a text prompt.
// Used by the vision extraction pipeline to send a batch of PDF pages to the LLM.
func MultiImageMessage(pages []PDFPageImage, text string) ChatMessage {
	parts := make([]contentPart, 0, len(pages)+1)
	for _, pg := range pages {
		b64 := base64.StdEncoding.EncodeToString(pg.Data)
		dataURL := fmt.Sprintf("data:image/jpeg;base64,%s", b64)
		parts = append(parts, contentPart{
			Type:     "image_url",
			ImageURL: &imageURL{URL: dataURL, Detail: "high"},
		})
	}
	if text != "" {
		parts = append(parts, contentPart{Type: "text", Text: text})
	}
	return ChatMessage{Role: "user", Content: parts}
}

// ImageMessage returns a user message containing an image and optional text
func ImageMessage(imageData []byte, mimeType, text string) ChatMessage {
	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
	parts := []contentPart{
		{
			Type: "image_url",
			ImageURL: &imageURL{
				URL:    dataURL,
				Detail: "high",
			},
		},
	}
	if text != "" {
		parts = append(parts, contentPart{Type: "text", Text: text})
	}
	return ChatMessage{Role: "user", Content: parts}
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// CheckAvailability tests the connection to the LLM API by listing models
func (c *LLMClient) CheckAvailability() CheckResult {
	u, err := url.JoinPath(c.BaseURL, "/models")
	if err != nil {
		return CheckResult{OK: false, Message: err.Error()}
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return CheckResult{OK: false, Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CheckResult{OK: false, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return CheckResult{OK: true, Message: fmt.Sprintf("LLM API reachable (HTTP %d)", resp.StatusCode)}
	}
	return CheckResult{OK: false, Message: fmt.Sprintf("LLM API returned HTTP %d", resp.StatusCode)}
}

// Chat sends a single or multi-turn conversation and returns the assistant reply.
// Pass the full message history including system, prior user/assistant turns, and the
// latest user message. Providers that support prompt caching will automatically cache
// repeated system prompts and prior context.
func (c *LLMClient) Chat(model string, messages []ChatMessage, maxTokens int) (string, error) {
	reqBody := chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   maxTokens,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	u, err := url.JoinPath(c.BaseURL, "/chat/completions")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", u, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse LLM response: %w (body: %s)", err, string(body))
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("LLM API error (%s): %s", chatResp.Error.Type, chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices (body: %s)", string(body))
	}
	return chatResp.Choices[0].Message.Content, nil
}

// OCRPage sends a single page image to the LLM for text extraction.
// Returns the raw OCR text for that page.
func (c *LLMClient) OCRPage(model string, imageData []byte, mimeType string, maxTokens int) (string, error) {
	if mimeType == "" {
		mimeType = "image/jpeg"
	}

	messages := []ChatMessage{
		ImageMessage(imageData, mimeType,
			"Please perform OCR on this document page. Extract all visible text and format it as clean Markdown, preserving headings, tables, and lists where present. Output only the Markdown text, no explanation or commentary."),
	}

	log.Printf("[LLM/OCR] Sending page (%d bytes, %s) to model %s", len(imageData), mimeType, model)
	result, err := c.Chat(model, messages, maxTokens)
	if err != nil {
		return "", fmt.Errorf("OCR call failed: %w", err)
	}
	return stripCodeFence(result), nil
}

// stripCodeFence removes leading ```markdown / ``` fences if present
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.SplitN(s, "\n", 2)
		if len(lines) == 2 {
			s = lines[1]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}
