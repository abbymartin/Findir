package parsers

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

type MarkupParser struct{}

func (m *MarkupParser) Extensions() []string {
	return []string{".html", ".htm", ".xhtml", ".xml", ".svg", ".docx", ".pptx"}
}

func (m *MarkupParser) Parse(fpath string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(fpath))

	var text string
	var err error

	switch ext {
	case ".html", ".htm", ".xhtml":
		text, err = parseHTML(fpath)
	case ".xml", ".svg":
		text, err = parseXML(fpath)
	case ".docx":
		text, err = parseDOCX(fpath)
	case ".pptx":
		text, err = parsePPTX(fpath)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	fullText := filepath.Base(fpath) + " " + text
	cleaned := cleanText(fullText, ext)

	if strings.TrimSpace(cleaned) == "" {
		return nil, nil
	}

	return ChunkText(cleaned, txtMaxChars), nil
}

func parseHTML(fpath string) (string, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	return extractHTMLText(f), nil
}

func extractHTMLText(r io.Reader) string {
	tokenizer := html.NewTokenizer(r)
	var b strings.Builder
	skipDepth := 0 // track depth inside script/style tags

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return b.String()

		case html.StartTagToken:
			tn, hasAttr := tokenizer.TagName()
			tagName := string(tn)

			if tagName == "script" || tagName == "style" {
				skipDepth++
			}

			// Extract alt and title attributes
			if hasAttr {
				for {
					key, val, more := tokenizer.TagAttr()
					k := string(key)
					if k == "alt" || k == "title" {
						v := strings.TrimSpace(string(val))
						if v != "" {
							b.WriteString(v)
							b.WriteByte(' ')
						}
					}
					if !more {
						break
					}
				}
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)
			if (tagName == "script" || tagName == "style") && skipDepth > 0 {
				skipDepth--
			}
			// Add spacing after block elements
			if isBlockElement(tagName) {
				b.WriteByte('\n')
			}

		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			text := strings.TrimSpace(string(tokenizer.Text()))
			if text != "" {
				b.WriteString(text)
				b.WriteByte(' ')
			}
		}
	}
}

func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6",
		"li", "tr", "br", "hr", "blockquote", "pre",
		"section", "article", "header", "footer", "nav":
		return true
	}
	return false
}

func parseXML(fpath string) (string, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	return extractXMLText(f), nil
}

func extractXMLText(r io.Reader) string {
	decoder := xml.NewDecoder(r)
	decoder.Strict = false
	var b strings.Builder

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" {
				b.WriteString(text)
				b.WriteByte(' ')
			}
		case xml.StartElement:
			for _, attr := range t.Attr {
				if attr.Name.Local == "alt" || attr.Name.Local == "title" {
					v := strings.TrimSpace(attr.Value)
					if v != "" {
						b.WriteString(v)
						b.WriteByte(' ')
					}
				}
			}
		}
	}

	return b.String()
}

func parseDOCX(fpath string) (string, error) {
	r, err := zip.OpenReader(fpath)
	if err != nil {
		return "", fmt.Errorf("opening docx: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("opening document.xml: %w", err)
			}
			defer rc.Close()
			return extractDocxText(rc), nil
		}
	}

	return "", nil
}

func extractDocxText(r io.Reader) string {
	decoder := xml.NewDecoder(r)
	var b strings.Builder
	inText := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			// <w:t> contains text runs in DOCX
			if t.Name.Local == "t" {
				inText = true
			}
			// <w:p> is a paragraph — add newline before
			if t.Name.Local == "p" && b.Len() > 0 {
				b.WriteString("\n\n")
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
				b.WriteByte(' ')
			}
		case xml.CharData:
			if inText {
				b.WriteString(string(t))
			}
		}
	}

	return b.String()
}

func parsePPTX(fpath string) (string, error) {
	r, err := zip.OpenReader(fpath)
	if err != nil {
		return "", fmt.Errorf("opening pptx: %w", err)
	}
	defer r.Close()

	var b strings.Builder
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b.WriteString(extractPptxSlideText(rc))
			rc.Close()
			b.WriteByte('\n')
		}
	}

	return b.String(), nil
}

func extractPptxSlideText(r io.Reader) string {
	decoder := xml.NewDecoder(r)
	var b strings.Builder
	inText := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			// <a:t> contains text runs in PPTX
			if t.Name.Local == "t" {
				inText = true
			}
			// <a:p> is a paragraph
			if t.Name.Local == "p" && b.Len() > 0 {
				b.WriteString("\n\n")
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
				b.WriteByte(' ')
			}
		case xml.CharData:
			if inText {
				b.WriteString(string(t))
			}
		}
	}

	return b.String()
}
