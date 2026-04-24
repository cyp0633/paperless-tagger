package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ExtractionPipeline extracts structured document fields from OCR text
type ExtractionPipeline struct {
	llm       *LLMClient
	paperless *PaperlessClient
}

// NewExtractionPipeline creates a new extraction pipeline
func NewExtractionPipeline(llm *LLMClient, paperless *PaperlessClient) *ExtractionPipeline {
	return &ExtractionPipeline{llm: llm, paperless: paperless}
}

// needMoreResponse is the sentinel the LLM returns when it wants to see more pages
type needMoreResponse struct {
	NeedMore bool `json:"_need_more"`
}

// BuildSystemPrompt substitutes live data into the prompt template
func (p *ExtractionPipeline) BuildSystemPrompt(template string) (string, error) {
	tags, err := p.paperless.GetTags()
	if err != nil {
		log.Printf("[Extractor] Warning: could not fetch tags: %v", err)
	}
	correspondents, err := p.paperless.GetCorrespondents()
	if err != nil {
		log.Printf("[Extractor] Warning: could not fetch correspondents: %v", err)
	}
	docTypes, err := p.paperless.GetDocumentTypes()
	if err != nil {
		log.Printf("[Extractor] Warning: could not fetch document types: %v", err)
	}

	tagNames := make([]string, len(tags))
	for i, t := range tags {
		tagNames[i] = t.Name
	}
	corrNames := make([]string, len(correspondents))
	for i, c := range correspondents {
		corrNames[i] = c.Name
	}
	dtNames := make([]string, len(docTypes))
	for i, dt := range docTypes {
		dtNames[i] = dt.Name
	}

	prompt := template
	prompt = strings.ReplaceAll(prompt, "{tags}", strings.Join(tagNames, ", "))
	prompt = strings.ReplaceAll(prompt, "{correspondents}", strings.Join(corrNames, ", "))
	prompt = strings.ReplaceAll(prompt, "{document_types}", strings.Join(dtNames, ", "))
	return prompt, nil
}

// Run performs iterative multi-round extraction. When settings.ExtrLLMVision is true
// it sends rendered page images directly to the LLM; otherwise it sends the OCR text.
//
//  1. Build the system prompt with live Paperless data
//  2. Send the first batch of pages (images or text) to the LLM
//  3. If the LLM responds with {"_need_more": true}, append the next batch and continue
//  4. Repeat up to settings.ExtrMaxRounds times
//  5. The full conversation history is passed on every call so providers can cache context
func (p *ExtractionPipeline) Run(doc *PaperlessDocument, ocrResult *OCRResult, settings *Settings) (*ExtractedFields, error) {
	systemPrompt, err := p.BuildSystemPrompt(EffectiveSystemPrompt(settings))
	if err != nil {
		return nil, fmt.Errorf("failed to build system prompt: %w", err)
	}

	maxRounds := settings.ExtrMaxRounds
	if maxRounds <= 0 {
		maxRounds = 5
	}

	if settings.ExtrLLMVision {
		return p.runVision(doc, ocrResult, systemPrompt, maxRounds, settings)
	}
	return p.runText(doc, ocrResult, systemPrompt, maxRounds, settings)
}

// runText sends OCR text batches to a non-vision LLM for field extraction.
func (p *ExtractionPipeline) runText(doc *PaperlessDocument, ocrResult *OCRResult, systemPrompt string, maxRounds int, settings *Settings) (*ExtractedFields, error) {
	charsPerBatch := settings.ExtrMaxInputChars
	if charsPerBatch <= 0 {
		charsPerBatch = 12000
	}

	batches := buildBatches(ocrResult, charsPerBatch)
	log.Printf("[Extractor] Document %d (text): %d pages → %d batch(es)", doc.ID, len(ocrResult.Pages), len(batches))

	history := []ChatMessage{TextMessage("system", systemPrompt)}
	return p.runRounds(doc, history, len(batches), maxRounds, settings,
		func(batchIdx int) ChatMessage {
			batchText := batches[batchIdx]
			if batchIdx == 0 {
				return TextMessage("user", fmt.Sprintf(
					"Here is the document text (pages %d of %d total):\n\n%s",
					min(batchIdx+1, len(ocrResult.Pages)), len(ocrResult.Pages), batchText))
			}
			return TextMessage("user", fmt.Sprintf("Here are the next pages:\n\n%s", batchText))
		})
}

// runVision sends batches of rendered page images to a vision LLM for field extraction.
func (p *ExtractionPipeline) runVision(doc *PaperlessDocument, ocrResult *OCRResult, systemPrompt string, maxRounds int, settings *Settings) (*ExtractedFields, error) {
	pagesPerBatch := settings.ExtrPagesPerBatch
	if pagesPerBatch <= 0 {
		pagesPerBatch = 5
	}

	batches := buildImageBatches(ocrResult.PageImages, pagesPerBatch)
	log.Printf("[Extractor] Document %d (vision): %d pages → %d batch(es)", doc.ID, len(ocrResult.PageImages), len(batches))

	history := []ChatMessage{TextMessage("system", systemPrompt)}
	totalPages := len(ocrResult.PageImages)
	return p.runRounds(doc, history, len(batches), maxRounds, settings,
		func(batchIdx int) ChatMessage {
			batch := batches[batchIdx]
			endPage := (batchIdx + 1) * pagesPerBatch
			if endPage > totalPages {
				endPage = totalPages
			}
			var prompt string
			if batchIdx == 0 {
				prompt = fmt.Sprintf("Here are the document pages (%d–%d of %d total):",
					batchIdx*pagesPerBatch+1, endPage, totalPages)
			} else {
				prompt = fmt.Sprintf("Here are the next pages (%d–%d of %d):",
					batchIdx*pagesPerBatch+1, endPage, totalPages)
			}
			return MultiImageMessage(batch, prompt)
		})
}

// runRounds drives the shared multi-round conversation loop used by both runText and runVision.
// nextBatchMsg is a factory that returns the user message for a given batch index.
func (p *ExtractionPipeline) runRounds(
	doc *PaperlessDocument,
	history []ChatMessage,
	totalBatches int,
	maxRounds int,
	settings *Settings,
	nextBatchMsg func(batchIdx int) ChatMessage,
) (*ExtractedFields, error) {
	var fields *ExtractedFields
	batchIdx := 0

	for round := 0; round < maxRounds; round++ {
		if batchIdx >= totalBatches {
			log.Printf("[Extractor] Document %d: no more pages, asking for best-effort answer", doc.ID)
			history = append(history, TextMessage("user",
				"No more pages are available. Please provide your best answer with the information you have seen so far. Respond with the fields JSON now."))
		} else {
			history = append(history, nextBatchMsg(batchIdx))
			batchIdx++
		}

		log.Printf("[Extractor] Document %d: round %d/%d, %d messages in context",
			doc.ID, round+1, maxRounds, len(history))

		raw, err := p.llm.Chat(settings.ExtrLLMModel, history, settings.ExtrMaxTokens)
		if err != nil {
			return nil, fmt.Errorf("extraction round %d failed: %w", round+1, err)
		}

		history = append(history, TextMessage("assistant", raw))
		cleaned := stripJSONFence(raw)

		var probe needMoreResponse
		if json.Unmarshal([]byte(cleaned), &probe) == nil && probe.NeedMore {
			if batchIdx >= totalBatches {
				log.Printf("[Extractor] Document %d: LLM requests more pages but none left", doc.ID)
			} else {
				log.Printf("[Extractor] Document %d: LLM requested more pages (round %d)", doc.ID, round+1)
			}
			continue
		}

		var result ExtractedFields
		if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
			log.Printf("[Extractor] Document %d: round %d parse error: %v (raw: %s)", doc.ID, round+1, err, cleaned)
			history = append(history, TextMessage("user",
				fmt.Sprintf("Your response could not be parsed as JSON: %v\nPlease respond again with only a valid JSON object.", err)))
			continue
		}

		fields = &result
		log.Printf("[Extractor] Document %d: extraction complete in %d round(s)", doc.ID, round+1)
		break
	}

	if fields == nil {
		return nil, fmt.Errorf("extraction did not produce a result after %d rounds", maxRounds)
	}

	log.Printf("[Extractor] Document %d: title=%q correspondent=%q tags=%v date=%q type=%q lang=%q",
		doc.ID, fields.Title, fields.Correspondent, fields.Tags,
		fields.DocumentDate, fields.DocumentType, fields.Language)
	return fields, nil
}

// buildImageBatches splits PDFPageImages into slices of at most pagesPerBatch images each.
func buildImageBatches(images []PDFPageImage, pagesPerBatch int) [][]PDFPageImage {
	if pagesPerBatch <= 0 {
		pagesPerBatch = 5
	}
	var batches [][]PDFPageImage
	for i := 0; i < len(images); i += pagesPerBatch {
		end := i + pagesPerBatch
		if end > len(images) {
			end = len(images)
		}
		batches = append(batches, images[i:end])
	}
	return batches
}

// buildBatches splits OCR pages into text batches of at most maxChars characters each.
// Each batch is a string containing one or more pages with separators.
func buildBatches(ocrResult *OCRResult, maxChars int) []string {
	var batches []string
	var current strings.Builder

	for i, pageText := range ocrResult.Pages {
		pageHeader := fmt.Sprintf("--- Page %d ---\n\n", i+1)
		chunk := pageHeader + pageText

		// If adding this page would exceed the limit and we already have content, flush
		if current.Len() > 0 && current.Len()+len(chunk)+2 > maxChars {
			batches = append(batches, current.String())
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(chunk)
	}

	if current.Len() > 0 {
		batches = append(batches, current.String())
	}
	return batches
}

// stripJSONFence removes ```json ... ``` fences around JSON responses
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.SplitN(s, "\n", 2)
		if len(lines) == 2 {
			s = lines[1]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
