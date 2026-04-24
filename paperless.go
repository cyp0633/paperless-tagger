package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PaperlessClient handles communication with the Paperless-ngx API
type PaperlessClient struct {
	BaseURL    string
	APIKey     string
	httpClient *http.Client
}

// NewPaperlessClient creates a new client with the given credentials
func NewPaperlessClient(baseURL, apiKey string) *PaperlessClient {
	return &PaperlessClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *PaperlessClient) newRequest(method, apiPath string, body io.Reader) (*http.Request, error) {
	// Split off any query string before joining with the base URL, because
	// url.JoinPath percent-encodes '?' and '&' when they appear in path segments.
	pathOnly, rawQuery, _ := strings.Cut(apiPath, "?")

	joined, err := url.JoinPath(c.BaseURL, pathOnly)
	if err != nil {
		return nil, err
	}
	if rawQuery != "" {
		joined += "?" + rawQuery
	}

	req, err := http.NewRequest(method, joined, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *PaperlessClient) do(req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req)
}

// CheckAvailability tests the connection to Paperless-ngx
func (c *PaperlessClient) CheckAvailability() CheckResult {
	req, err := c.newRequest("GET", "/api/profile/", nil)
	if err != nil {
		return CheckResult{OK: false, Message: err.Error()}
	}
	resp, err := c.do(req)
	if err != nil {
		return CheckResult{OK: false, Message: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return CheckResult{OK: false, Message: fmt.Sprintf("Authentication failed (HTTP %d): invalid API key for this Paperless server", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return CheckResult{OK: false, Message: fmt.Sprintf("Unexpected status: %d", resp.StatusCode)}
	}

	var profile struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Connected but invalid response from /api/profile/: %v", err)}
	}
	if profile.Username == "" {
		return CheckResult{OK: false, Message: "Connected but no authenticated user returned by /api/profile/"}
	}

	return CheckResult{OK: true, Message: fmt.Sprintf("Authenticated as %q", profile.Username)}
}

// GetUntaggedDocuments retrieves documents that have no tags
func (c *PaperlessClient) GetUntaggedDocuments() ([]PaperlessDocument, error) {
	var results []PaperlessDocument
	page := 1
	for {
		path := fmt.Sprintf("/api/documents/?is_tagged=false&page=%d&page_size=100", page)
		req, err := c.newRequest("GET", path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("paperless API returned %d for untagged documents", resp.StatusCode)
		}
		var list PaperlessListResponse[PaperlessDocument]
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return nil, err
		}
		results = append(results, list.Results...)
		if list.Next == "" {
			break
		}
		page++
	}
	log.Printf("[Paperless] Found %d untagged documents", len(results))
	return results, nil
}

// GetDocument retrieves a single document by ID
func (c *PaperlessClient) GetDocument(id int) (*PaperlessDocument, error) {
	req, err := c.newRequest("GET", fmt.Sprintf("/api/documents/%d/", id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("paperless API returned %d for document %d", resp.StatusCode, id)
	}
	var doc PaperlessDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DownloadDocumentImage downloads the original file of a document (for vision OCR)
func (c *PaperlessClient) DownloadDocumentImage(id int) ([]byte, string, error) {
	req, err := c.newRequest("GET", fmt.Sprintf("/api/documents/%d/download/", id), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("paperless download returned %d for document %d", resp.StatusCode, id)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// GetTags retrieves all tags from Paperless-ngx
func (c *PaperlessClient) GetTags() ([]PaperlessTag, error) {
	return fetchAll[PaperlessTag](c, "/api/tags/?page_size=500")
}

// GetCorrespondents retrieves all correspondents from Paperless-ngx
func (c *PaperlessClient) GetCorrespondents() ([]PaperlessCorrespondent, error) {
	return fetchAll[PaperlessCorrespondent](c, "/api/correspondents/?page_size=500")
}

// GetDocumentTypes retrieves all document types from Paperless-ngx
func (c *PaperlessClient) GetDocumentTypes() ([]PaperlessDocumentType, error) {
	return fetchAll[PaperlessDocumentType](c, "/api/document_types/?page_size=500")
}

// FindOrCreateTag finds a tag by name or creates it, returning its ID
func (c *PaperlessClient) FindOrCreateTag(name string) (int, error) {
	tags, err := c.GetTags()
	if err != nil {
		return 0, err
	}
	for _, t := range tags {
		if t.Name == name {
			return t.ID, nil
		}
	}
	// Create new tag
	payload := map[string]string{"name": name}
	data, _ := json.Marshal(payload)
	req, err := c.newRequest("POST", "/api/tags/", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var tag PaperlessTag
	if err := json.NewDecoder(resp.Body).Decode(&tag); err != nil {
		return 0, err
	}
	return tag.ID, nil
}

// FindOrCreateCorrespondent finds a correspondent by name or creates it, returning its ID
func (c *PaperlessClient) FindOrCreateCorrespondent(name string) (int, error) {
	correspondents, err := c.GetCorrespondents()
	if err != nil {
		return 0, err
	}
	for _, co := range correspondents {
		if co.Name == name {
			return co.ID, nil
		}
	}
	payload := map[string]string{"name": name}
	data, _ := json.Marshal(payload)
	req, err := c.newRequest("POST", "/api/correspondents/", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var co PaperlessCorrespondent
	if err := json.NewDecoder(resp.Body).Decode(&co); err != nil {
		return 0, err
	}
	return co.ID, nil
}

// FindOrCreateDocumentType finds a document type by name or creates it, returning its ID
func (c *PaperlessClient) FindOrCreateDocumentType(name string) (int, error) {
	types, err := c.GetDocumentTypes()
	if err != nil {
		return 0, err
	}
	for _, dt := range types {
		if dt.Name == name {
			return dt.ID, nil
		}
	}
	payload := map[string]string{"name": name}
	data, _ := json.Marshal(payload)
	req, err := c.newRequest("POST", "/api/document_types/", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var dt PaperlessDocumentType
	if err := json.NewDecoder(resp.Body).Decode(&dt); err != nil {
		return 0, err
	}
	return dt.ID, nil
}

// UpdateDocument patches a document with the extracted fields
func (c *PaperlessClient) UpdateDocument(docID int, fields *ExtractedFields) error {
	payload := DocumentUpdatePayload{}

	if fields.Title != "" {
		payload.Title = fields.Title
	}
	if fields.DocumentDate != "" {
		payload.CreatedDate = fields.DocumentDate
	}

	if fields.Correspondent != "" {
		id, err := c.FindOrCreateCorrespondent(fields.Correspondent)
		if err != nil {
			log.Printf("[Paperless] Warning: could not find/create correspondent %q: %v", fields.Correspondent, err)
		} else {
			payload.Correspondent = &id
		}
	}

	if fields.DocumentType != "" {
		id, err := c.FindOrCreateDocumentType(fields.DocumentType)
		if err != nil {
			log.Printf("[Paperless] Warning: could not find/create document type %q: %v", fields.DocumentType, err)
		} else {
			payload.DocumentType = &id
		}
	}

	for _, tagName := range fields.Tags {
		if tagName == "" {
			continue
		}
		id, err := c.FindOrCreateTag(tagName)
		if err != nil {
			log.Printf("[Paperless] Warning: could not find/create tag %q: %v", tagName, err)
			continue
		}
		payload.Tags = append(payload.Tags, id)
	}

	if fields.OCRContent != "" {
		payload.Content = fields.OCRContent
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := c.newRequest("PATCH", fmt.Sprintf("/api/documents/%d/", docID), bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("paperless PATCH returned %d: %s", resp.StatusCode, string(body))
	}
	log.Printf("[Paperless] Document %d updated successfully", docID)
	return nil
}

// fetchAll is a generic helper to fetch all pages of a paginated resource
func fetchAll[T any](c *PaperlessClient, path string) ([]T, error) {
	var results []T
	currentPath := path
	for currentPath != "" {
		req, err := c.newRequest("GET", currentPath, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned %d for %s", resp.StatusCode, currentPath)
		}
		var list PaperlessListResponse[T]
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return nil, err
		}
		results = append(results, list.Results...)
		// Extract path from next URL
		if list.Next != "" {
			u, err := url.Parse(list.Next)
			if err != nil {
				break
			}
			currentPath = u.RequestURI()
		} else {
			currentPath = ""
		}
	}
	return results, nil
}
