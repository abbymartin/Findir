package parsers

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const minPdfTextLen = 50

type PdfParser struct{}

func (p *PdfParser) Extensions() []string {
	return []string{".pdf"}
}

func (p *PdfParser) Parse(fpath string) ([]string, error) {
	// pdftotext <file> - outputs to stdout
	cmd := exec.Command("pdftotext", fpath, "-")
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("pdftotext"); lookErr != nil {
			return nil, fmt.Errorf("pdftotext not found. Install with: sudo apt install poppler-utils")
		}
		return nil, fmt.Errorf("extracting text from PDF: %w", err)
	}

	text := string(out)

	// Skip PDFs with minimal text (likely scanned/image-only)
	stripped := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, text)
	if len(stripped) < minPdfTextLen {
		return nil, nil
	}

	fullText := filepath.Base(fpath) + " " + text
	cleaned := cleanText(fullText, ".pdf")

	if strings.TrimSpace(cleaned) == "" {
		return nil, nil
	}

	return ChunkText(cleaned, txtMaxChars), nil
}
