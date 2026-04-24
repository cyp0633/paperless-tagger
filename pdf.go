package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	pdfMaxDimension = 2048 // longest edge in pixels (passed to pdftoppm -scale-to)
	pdfJPEGQuality  = 85
)

// PDFPageImage holds rendered JPEG bytes for one page
type PDFPageImage struct {
	PageNumber int    // 0-based
	Data       []byte // JPEG bytes
	Width      int    // 0 when not known (pdftoppm path)
	Height     int    // 0 when not known (pdftoppm path)
}

// PDFToImages renders a PDF into per-page JPEG images using pdftoppm.
// maxPages == 0 means render all pages.
// startPage is 0-based.
func PDFToImages(pdfData []byte, startPage, maxPages int) ([]PDFPageImage, int, error) {
	// Write PDF bytes to a temp file (pdftoppm needs a file path)
	tmp, err := os.CreateTemp("", "ptagger-*.pdf")
	if err != nil {
		return nil, 0, fmt.Errorf("pdf temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(pdfData); err != nil {
		tmp.Close()
		return nil, 0, err
	}
	tmp.Close()

	// Get total page count via pdfinfo
	totalPages, err := pdfPageCount(tmp.Name())
	if err != nil {
		return nil, 0, fmt.Errorf("page count: %w", err)
	}

	if startPage >= totalPages {
		return nil, totalPages, nil
	}

	endPage := totalPages
	if maxPages > 0 && startPage+maxPages < totalPages {
		endPage = startPage + maxPages
	}

	// Render the requested page range to a temp directory
	outDir, err := os.MkdirTemp("", "ptagger-pages-*")
	if err != nil {
		return nil, 0, fmt.Errorf("output temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	prefix := filepath.Join(outDir, "page")

	// pdftoppm uses 1-based page numbers; -scale-to caps the longest edge
	args := []string{
		"-f", strconv.Itoa(startPage + 1),
		"-l", strconv.Itoa(endPage),
		"-jpeg",
		"-jpegopt", fmt.Sprintf("quality=%d", pdfJPEGQuality),
		"-scale-to", strconv.Itoa(pdfMaxDimension),
		tmp.Name(),
		prefix,
	}
	if out, err := exec.Command("pdftoppm", args...).CombinedOutput(); err != nil {
		return nil, 0, fmt.Errorf("pdftoppm: %w\n%s", err, string(out))
	}

	// Collect output files (pdftoppm names them <prefix>-NNN.jpg)
	files, err := filepath.Glob(filepath.Join(outDir, "*.jpg"))
	if err != nil {
		return nil, 0, fmt.Errorf("glob output: %w", err)
	}
	sort.Strings(files) // alphabetical order = page order

	var results []PDFPageImage
	for i, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, totalPages, fmt.Errorf("read page %d: %w", startPage+i, err)
		}
		results = append(results, PDFPageImage{
			PageNumber: startPage + i,
			Data:       data,
		})
	}

	return results, totalPages, nil
}

// pdfPageCount returns the total number of pages in a PDF using pdfinfo
func pdfPageCount(pdfPath string) (int, error) {
	out, err := exec.Command("pdfinfo", pdfPath).Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				n, err := strconv.Atoi(parts[1])
				if err == nil {
					return n, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("could not parse page count from pdfinfo output")
}
