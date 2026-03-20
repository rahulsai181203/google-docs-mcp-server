// docformat applies rich formatting and code-snippet styling to a Google Doc.
//
// Pass the document ID or full URL as the first argument:
//
//	go run ./cmd/docformat/ <document-id-or-url>
//
// What it does (two sequential passes):
//  1. Beautify – applies heading styles, colors, and removes ASCII separators.
//  2. Code snippets – adds shading, a blue left border, and Courier New to
//     every paragraph that contains shell commands or JSON config.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	docs "google.golang.org/api/docs/v1"
	"google.golang.org/api/option"

	"google-docs-mcp/internal/auth"
)

var sectionHeaders = map[string]bool{
	"PREREQUISITES":         true,
	"SETUP":                 true,
	"ENVIRONMENT VARIABLES": true,
	"AVAILABLE TOOLS":       true,
	"NOTES":                 true,
}

var toolNames = map[string]bool{
	"get_document":         true,
	"create_document":      true,
	"append_text":          true,
	"insert_text":          true,
	"replace_text":         true,
	"delete_content_range": true,
	"list_documents":       true,
	"search_documents":     true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: docformat <document-id-or-url>")
		os.Exit(1)
	}
	docID := extractDocID(os.Args[1])

	ctx := context.Background()

	httpClient, err := auth.NewGoogleClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
		os.Exit(1)
	}

	svc, err := docs.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs service error: %v\n", err)
		os.Exit(1)
	}

	if err := runBeautify(ctx, svc, docID); err != nil {
		fmt.Fprintf(os.Stderr, "beautify error: %v\n", err)
		os.Exit(1)
	}

	if err := runCodeSnippets(ctx, svc, docID); err != nil {
		fmt.Fprintf(os.Stderr, "code snippets error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Document formatted successfully.")
}

// ── Pass 1: beautify ──────────────────────────────────────────────────────────

func runBeautify(ctx context.Context, svc *docs.Service, docID string) error {
	doc, err := svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	formatReqs, deleteReqs := buildBeautifyRequests(doc)

	if len(formatReqs) > 0 {
		if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
			Requests: formatReqs,
		}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("apply formatting: %w", err)
		}
		fmt.Printf("Applied %d formatting operations.\n", len(formatReqs))
	}

	if len(deleteReqs) > 0 {
		sort.Slice(deleteReqs, func(i, j int) bool {
			return deleteReqs[i].DeleteContentRange.Range.StartIndex >
				deleteReqs[j].DeleteContentRange.Range.StartIndex
		})
		if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
			Requests: deleteReqs,
		}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("delete separators: %w", err)
		}
		fmt.Printf("Removed %d separator lines.\n", len(deleteReqs))
	}

	return nil
}

func buildBeautifyRequests(doc *docs.Document) (formatReqs, deleteReqs []*docs.Request) {
	for _, elem := range doc.Body.Content {
		if elem.Paragraph == nil {
			continue
		}
		text := strings.TrimRight(paragraphText(elem.Paragraph), "\n")
		start, end := elem.StartIndex, elem.EndIndex

		switch {
		case text == "Google Docs MCP Server — Usage Guide":
			formatReqs = append(formatReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{
					NamedStyleType: "HEADING_1",
					Alignment:      "CENTER",
				}, "namedStyleType,alignment"),
				textStyleReq(start, end-1, &docs.TextStyle{
					FontSize:        pt(24),
					Bold:            true,
					ForegroundColor: rgb(0x1a, 0x73, 0xe8),
				}, "fontSize,bold,foregroundColor"),
			)

		case strings.HasPrefix(text, "────"):
			deleteReqs = append(deleteReqs, &docs.Request{
				DeleteContentRange: &docs.DeleteContentRangeRequest{
					Range: &docs.Range{StartIndex: start, EndIndex: end},
				},
			})

		case sectionHeaders[text]:
			formatReqs = append(formatReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{NamedStyleType: "HEADING_2"}, "namedStyleType"),
				textStyleReq(start, end-1, &docs.TextStyle{
					ForegroundColor: rgb(0x1a, 0x73, 0xe8),
					Bold:            true,
					FontSize:        pt(15),
				}, "foregroundColor,bold,fontSize"),
			)

		case strings.HasPrefix(text, "Step "):
			formatReqs = append(formatReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{NamedStyleType: "HEADING_3"}, "namedStyleType"),
				textStyleReq(start, end-1, &docs.TextStyle{
					ForegroundColor: rgb(0x18, 0x65, 0xf1),
					Bold:            true,
				}, "foregroundColor,bold"),
			)

		case toolNames[text]:
			formatReqs = append(formatReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{NamedStyleType: "HEADING_3"}, "namedStyleType"),
				textStyleReq(start, end-1, &docs.TextStyle{
					WeightedFontFamily: mono(),
					ForegroundColor:    rgb(0xc0, 0x39, 0x2b),
					Bold:               true,
					FontSize:           pt(12),
				}, "weightedFontFamily,foregroundColor,bold,fontSize"),
			)

		case strings.TrimSpace(text) == "Parameters:":
			formatReqs = append(formatReqs,
				textStyleReq(start, end-1, &docs.TextStyle{Bold: true}, "bold"),
			)

		case strings.HasPrefix(text, "GitHub:"):
			formatReqs = append(formatReqs,
				textStyleReq(start, start+7, &docs.TextStyle{Bold: true}, "bold"),
			)
		}
	}
	return
}

// ── Pass 2: code snippets ─────────────────────────────────────────────────────

func runCodeSnippets(ctx context.Context, svc *docs.Service, docID string) error {
	doc, err := svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	reqs := buildCodeSnippetRequests(doc)
	if len(reqs) == 0 {
		return nil
	}

	if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: reqs,
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("apply code snippets: %w", err)
	}

	fmt.Printf("Styled %d code paragraphs.\n", len(reqs)/2)
	return nil
}

func buildCodeSnippetRequests(doc *docs.Document) []*docs.Request {
	var reqs []*docs.Request
	for _, elem := range doc.Body.Content {
		if elem.Paragraph == nil {
			continue
		}
		text := strings.TrimRight(paragraphText(elem.Paragraph), "\n")
		start, end := elem.StartIndex, elem.EndIndex

		if !isCodeParagraph(text) {
			continue
		}

		reqs = append(reqs,
			paraStyleReq(start, end, &docs.ParagraphStyle{
				Shading: &docs.Shading{BackgroundColor: rgb(0xf6, 0xf8, 0xfa)},
				BorderLeft: &docs.ParagraphBorder{
					Color:     rgb(0x1a, 0x73, 0xe8),
					Width:     &docs.Dimension{Magnitude: 3, Unit: "PT"},
					Padding:   &docs.Dimension{Magnitude: 4, Unit: "PT"},
					DashStyle: "SOLID",
				},
				IndentStart: &docs.Dimension{Magnitude: 24, Unit: "PT"},
				SpaceAbove:  &docs.Dimension{Magnitude: 4, Unit: "PT"},
				SpaceBelow:  &docs.Dimension{Magnitude: 4, Unit: "PT"},
			}, "shading,borderLeft,indentStart,spaceAbove,spaceBelow"),
			textStyleReq(start, end-1, &docs.TextStyle{
				WeightedFontFamily: mono(),
				ForegroundColor:    rgb(0x24, 0x29, 0x2e),
				BackgroundColor:    rgb(0xf6, 0xf8, 0xfa),
				FontSize:           pt(10),
			}, "weightedFontFamily,foregroundColor,backgroundColor,fontSize"),
		)
	}
	return reqs
}

func isCodeParagraph(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	if strings.HasPrefix(text, "   ") {
		notCode := []string{
			"(required)", "(optional)", "Or set", "Parameters",
			"Read the", "Create a", "Append text", "Insert text",
			"Replace all", "Delete a", "Delete content", "List ", "Search for",
			"This opens", "Service account", "Restart Claude",
		}
		trimmed := strings.TrimSpace(text)
		for _, phrase := range notCode {
			if strings.Contains(trimmed, phrase) {
				return false
			}
		}
		codeSignals := []string{
			"cd ", "go ", "./", "export ", "{", "}",
			"\"command\"", "\"mcpServers\"", "\"google-docs\"", "\"env\"",
			"/path/to", "/absolute/",
			"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_OAUTH_CREDENTIALS",
		}
		for _, sig := range codeSignals {
			if strings.Contains(text, sig) {
				return true
			}
		}
	}
	if strings.HasPrefix(text, "GOOGLE_") {
		return true
	}
	if strings.Contains(text, "~/.config/google-docs-mcp/") ||
		strings.Contains(text, "claude_desktop_config.json") {
		return true
	}
	return false
}

// ── shared helpers ────────────────────────────────────────────────────────────

func paragraphText(p *docs.Paragraph) string {
	var sb strings.Builder
	for _, pe := range p.Elements {
		if pe.TextRun != nil {
			sb.WriteString(pe.TextRun.Content)
		}
	}
	return sb.String()
}

func paraStyleReq(start, end int64, style *docs.ParagraphStyle, fields string) *docs.Request {
	return &docs.Request{
		UpdateParagraphStyle: &docs.UpdateParagraphStyleRequest{
			Range:          &docs.Range{StartIndex: start, EndIndex: end},
			ParagraphStyle: style,
			Fields:         fields,
		},
	}
}

func textStyleReq(start, end int64, style *docs.TextStyle, fields string) *docs.Request {
	return &docs.Request{
		UpdateTextStyle: &docs.UpdateTextStyleRequest{
			Range:     &docs.Range{StartIndex: start, EndIndex: end},
			TextStyle: style,
			Fields:    fields,
		},
	}
}

func rgb(r, g, b uint8) *docs.OptionalColor {
	return &docs.OptionalColor{Color: &docs.Color{RgbColor: &docs.RgbColor{
		Red:   float64(r) / 255,
		Green: float64(g) / 255,
		Blue:  float64(b) / 255,
	}}}
}

func pt(size float64) *docs.Dimension {
	return &docs.Dimension{Magnitude: size, Unit: "PT"}
}

func mono() *docs.WeightedFontFamily {
	return &docs.WeightedFontFamily{FontFamily: "Courier New"}
}

func extractDocID(input string) string {
	const marker = "/document/d/"
	if idx := strings.Index(input, marker); idx != -1 {
		rest := input[idx+len(marker):]
		if slash := strings.Index(rest, "/"); slash != -1 {
			return rest[:slash]
		}
		return rest
	}
	return input
}
