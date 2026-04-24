package main

import (
	"fmt"
	"log"
	"strings"
)

// OCRPipeline handles extracting text from documents via LLM vision on rendered PDF pages
type OCRPipeline struct {
	llm       *LLMClient
	paperless *PaperlessClient
}

// NewOCRPipeline creates a new OCR pipeline
func NewOCRPipeline(llm *LLMClient, paperless *PaperlessClient) *OCRPipeline {
	return &OCRPipeline{llm: llm, paperless: paperless}
}

// OCRResult holds the concatenated OCR text for a document
type OCRResult struct {
	// Pages contains per-page OCR text in order
	Pages []string
	// PageImages contains the rendered JPEG bytes for each processed page, used when
	// the extraction LLM is vision-capable and should receive images directly.
	PageImages []PDFPageImage
	// TotalPages is the total number of pages in the document (may exceed len(Pages) if capped)
	TotalPages int
}

// Content returns the full concatenated OCR text with page separators
func (r *OCRResult) Content() string {
	if len(r.Pages) == 1 {
		return r.Pages[0]
	}
	var sb strings.Builder
	for i, p := range r.Pages {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "--- Page %d ---\n\n", i+1)
		sb.WriteString(p)
	}
	return sb.String()
}

// PageRange returns OCR content for pages [start, start+count), 0-based, for iterative extraction
func (r *OCRResult) PageRange(start, count int) string {
	end := start + count
	if end > len(r.Pages) {
		end = len(r.Pages)
	}
	if start >= end {
		return ""
	}
	pages := r.Pages[start:end]
	if len(pages) == 1 {
		return pages[0]
	}
	var sb strings.Builder
	for i, p := range pages {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "--- Page %d ---\n\n", start+i+1)
		sb.WriteString(p)
	}
	return sb.String()
}

// Run downloads the document, renders all pages to images, and OCRs each page.
// It always uses vision OCR; there is no fallback to Paperless' existing content
// because ocrmypdf has poor non-English support.
func (p *OCRPipeline) Run(doc *PaperlessDocument, settings *Settings) (*OCRResult, error) {
	log.Printf("[OCR] Document %d (%s): downloading file for vision OCR", doc.ID, doc.Title)

	fileData, _, err := p.paperless.DownloadDocumentImage(doc.ID)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	log.Printf("[OCR] Document %d: rendering PDF pages (max %d)", doc.ID, settings.OCRMaxPages)
	pages, totalPages, err := PDFToImages(fileData, 0, settings.OCRMaxPages)
	if err != nil {
		return nil, fmt.Errorf("PDF rendering failed: %w", err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("document has no renderable pages")
	}
	log.Printf("[OCR] Document %d: rendered %d/%d pages", doc.ID, len(pages), totalPages)

	result := &OCRResult{
		Pages:      make([]string, 0, len(pages)),
		PageImages: pages,
		TotalPages: totalPages,
	}

	for _, pg := range pages {
		log.Printf("[OCR] Document %d: OCR page %d (%d bytes)",
			doc.ID, pg.PageNumber+1, len(pg.Data))

		text, err := p.llm.OCRPage(settings.OCRLLMModel, pg.Data, "image/jpeg", settings.OCRMaxTokens)
		if err != nil {
			log.Printf("[OCR] Document %d: page %d OCR error: %v", doc.ID, pg.PageNumber+1, err)
			// Append a placeholder rather than failing the whole document
			result.Pages = append(result.Pages, fmt.Sprintf("[OCR error on page %d: %v]", pg.PageNumber+1, err))
			continue
		}
		result.Pages = append(result.Pages, text)
	}

	log.Printf("[OCR] Document %d: completed, %d pages of text extracted", doc.ID, len(result.Pages))
	return result, nil
}
