package parsers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

type langConfig struct {
	language *tree_sitter.Language
	query    string
}

var langConfigs map[string]*langConfig

func init() {
	configs := []struct {
		exts   []string
		lang   *tree_sitter.Language
		query  string
	}{
		{
			exts: []string{".go"},
			lang: tree_sitter.NewLanguage(tree_sitter_go.Language()),
			query: `
				(function_declaration name: (identifier) @name) @func
				(method_declaration name: (field_identifier) @name) @func
				(type_declaration (type_spec name: (type_identifier) @name)) @type
				(comment) @comment
			`,
		},
		{
			exts: []string{".py"},
			lang: tree_sitter.NewLanguage(tree_sitter_python.Language()),
			query: `
				(function_definition name: (identifier) @name) @func
				(class_definition name: (identifier) @name) @type
				(comment) @comment
				(expression_statement (string) @comment)
			`,
		},
		{
			exts: []string{".js", ".jsx"},
			lang: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
			query: `
				(function_declaration name: (identifier) @name) @func
				(class_declaration name: (identifier) @name) @type
				(method_definition name: (property_identifier) @name) @func
				(arrow_function) @func
				(comment) @comment
			`,
		},
		{
			exts: []string{".java"},
			lang: tree_sitter.NewLanguage(tree_sitter_java.Language()),
			query: `
				(method_declaration name: (identifier) @name) @func
				(class_declaration name: (identifier) @name) @type
				(interface_declaration name: (identifier) @name) @type
				(line_comment) @comment
				(block_comment) @comment
			`,
		},
		{
			exts: []string{".c", ".h"},
			lang: tree_sitter.NewLanguage(tree_sitter_c.Language()),
			query: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @func
				(struct_specifier name: (type_identifier) @name) @type
				(comment) @comment
			`,
		},
		{
			exts: []string{".cpp", ".cc", ".cxx", ".hpp", ".hxx"},
			lang: tree_sitter.NewLanguage(tree_sitter_cpp.Language()),
			query: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @func
				(class_specifier name: (type_identifier) @name) @type
				(struct_specifier name: (type_identifier) @name) @type
				(comment) @comment
			`,
		},
		{
			exts: []string{".rs"},
			lang: tree_sitter.NewLanguage(tree_sitter_rust.Language()),
			query: `
				(function_item name: (identifier) @name) @func
				(struct_item name: (type_identifier) @name) @type
				(impl_item type: (type_identifier) @name) @type
				(trait_item name: (type_identifier) @name) @type
				(line_comment) @comment
				(block_comment) @comment
			`,
		},
	}

	langConfigs = make(map[string]*langConfig)
	for _, cfg := range configs {
		lc := &langConfig{language: cfg.lang, query: cfg.query}
		for _, ext := range cfg.exts {
			langConfigs[ext] = lc
		}
	}
}

type CodeParser struct{}

func (c *CodeParser) Extensions() []string {
	exts := make([]string, 0, len(langConfigs))
	for ext := range langConfigs {
		exts = append(exts, ext)
	}
	return exts
}

func (c *CodeParser) Parse(fpath string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(fpath))
	cfg, ok := langConfigs[ext]
	if !ok {
		return nil, nil
	}

	source, err := os.ReadFile(fpath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	if len(strings.TrimSpace(string(source))) == 0 {
		return nil, nil
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(cfg.language); err != nil {
		return nil, fmt.Errorf("setting language: %w", err)
	}

	tree := parser.Parse(source, nil)
	defer tree.Close()

	query, queryErr := tree_sitter.NewQuery(cfg.language, cfg.query)
	if queryErr != nil {
		return nil, fmt.Errorf("compiling query: %s", queryErr.Message)
	}
	defer query.Close()

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), source)

	captureNames := query.CaptureNames()
	filename := filepath.Base(fpath)
	var chunks []string
	seen := make(map[string]bool) // deduplicate overlapping matches

	// Collect comments to associate with following definitions
	var pendingComments []string

	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			captureName := captureNames[capture.Index]
			text := capture.Node.Utf8Text(source)

			switch captureName {
			case "comment":
				// Clean comment markers
				cleaned := cleanComment(text)
				if cleaned != "" {
					pendingComments = append(pendingComments, cleaned)
				}

			case "func", "type":
				key := fmt.Sprintf("%d-%d", capture.Node.StartByte(), capture.Node.EndByte())
				if seen[key] {
					continue
				}
				seen[key] = true

				// Get the name if available
				name := ""
				for _, c := range match.Captures {
					if captureNames[c.Index] == "name" {
						name = c.Node.Utf8Text(source)
						break
					}
				}

				// Build chunk: filename + name + signature/first lines + comments
				var chunk strings.Builder
				chunk.WriteString(filename)
				if name != "" {
					chunk.WriteString(" " + name)
				}
				chunk.WriteString(": ")

				// Include associated comments
				if len(pendingComments) > 0 {
					chunk.WriteString(strings.Join(pendingComments, " "))
					chunk.WriteString(" ")
					pendingComments = nil
				}

				// Extract a meaningful preview of the definition
				preview := extractPreview(text, 300)
				chunk.WriteString(preview)

				result := chunk.String()
				if strings.TrimSpace(result) != "" {
					chunks = append(chunks, cleanText(result, ext))
				}

			case "name":
				// Handled when processing func/type captures
				continue
			}
		}
	}

	// If there are leftover comments (e.g., file-level comments), add them as a chunk
	if len(pendingComments) > 0 {
		chunk := filename + " " + strings.Join(pendingComments, " ")
		chunks = append(chunks, cleanText(chunk, ext))
	}

	if len(chunks) == 0 {
		return nil, nil
	}

	// Re-chunk any oversized entries
	var result []string
	for _, ch := range chunks {
		if len(ch) > txtMaxChars {
			result = append(result, ChunkText(ch, txtMaxChars)...)
		} else {
			result = append(result, ch)
		}
	}

	return result, nil
}

func cleanComment(text string) string {
	// Strip comment markers
	text = strings.TrimSpace(text)

	// Line comments
	if strings.HasPrefix(text, "//") {
		text = strings.TrimPrefix(text, "//")
		return strings.TrimSpace(text)
	}
	if strings.HasPrefix(text, "#") {
		text = strings.TrimPrefix(text, "#")
		return strings.TrimSpace(text)
	}

	// Block comments
	if strings.HasPrefix(text, "/*") {
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		// Clean up leading * on each line
		lines := strings.Split(text, "\n")
		var cleaned []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "* ")
			line = strings.TrimPrefix(line, "*")
			if line != "" {
				cleaned = append(cleaned, strings.TrimSpace(line))
			}
		}
		return strings.Join(cleaned, " ")
	}

	// Python docstrings
	if strings.HasPrefix(text, `"""`) || strings.HasPrefix(text, `'''`) {
		text = strings.Trim(text, `"'`)
		return strings.TrimSpace(text)
	}

	return text
}

func extractPreview(text string, maxLen int) string {
	// Take the first meaningful lines of a definition
	lines := strings.Split(text, "\n")
	var preview strings.Builder
	for _, line := range lines {
		if preview.Len()+len(line) > maxLen {
			break
		}
		if preview.Len() > 0 {
			preview.WriteByte(' ')
		}
		preview.WriteString(strings.TrimSpace(line))
	}
	return preview.String()
}
