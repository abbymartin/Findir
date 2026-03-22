package parsers

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Comments: % to end of line (but not \%)
	latexComment = regexp.MustCompile(`(?m)(?:^|[^\\])%.*$`)
	// Display math: $$...$$, \[...\]
	latexDisplayMath = regexp.MustCompile(`\$\$[\s\S]*?\$\$|\\\[[\s\S]*?\\]`)
	// Inline math: $...$, \(...\)
	latexInlineMath = regexp.MustCompile(`\$[^$]+?\$|\\\([\s\S]*?\\\)`)
	// Math environments: \begin{env}...\end{env} for each math environment
	latexMathEnv = regexp.MustCompile(`(?s)\\begin\{(?:equation|align|gather|multline|eqnarray)\*?\}.*?\\end\{(?:equation|align|gather|multline|eqnarray)\*?\}`)
	// \begin{...} and \end{...} markers (keep content between)
	latexEnvMarker = regexp.MustCompile(`\\(?:begin|end)\{[^}]*\}`)
	// \command{content} → keep content
	latexCmdWithBraces = regexp.MustCompile(`\\[a-zA-Z]+\{([^}]*)\}`)
	// \command without braces
	latexCmdAlone = regexp.MustCompile(`\\[a-zA-Z]+`)
)

type LatexParser struct{}

func (l *LatexParser) Extensions() []string {
	return []string{".tex"}
}

func (l *LatexParser) Parse(fpath string) ([]string, error) {
	content, err := os.ReadFile(fpath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	text := string(content)

	// Strip in order of specificity
	text = latexComment.ReplaceAllString(text, "")
	text = latexMathEnv.ReplaceAllString(text, "")
	text = latexDisplayMath.ReplaceAllString(text, "")
	text = latexInlineMath.ReplaceAllString(text, "")
	text = latexEnvMarker.ReplaceAllString(text, "")
	// Replace \command{content} with just content
	text = latexCmdWithBraces.ReplaceAllString(text, "$1")
	// Remove remaining bare commands
	text = latexCmdAlone.ReplaceAllString(text, "")

	fullText := filepath.Base(fpath) + " " + text
	cleaned := cleanText(fullText, ".tex")

	if strings.TrimSpace(cleaned) == "" {
		return nil, nil
	}

	return ChunkText(cleaned, txtMaxChars), nil
}
