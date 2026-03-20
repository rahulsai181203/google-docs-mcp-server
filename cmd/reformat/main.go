// reformat applies a clean, readable style to a Google Doc.
//
// Design goals:
//   - Single dark-charcoal palette — no competing accent colors
//   - Default Google heading hierarchy for size/weight
//   - Subtle code blocks: soft gray background, no harsh borders
//   - Generous spacing so the eye can breathe
//
// Usage:  go run ./cmd/reformat/ <document-id-or-url>
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	docs "google.golang.org/api/docs/v1"
	"google.golang.org/api/option"

	"google-docs-mcp/internal/auth"
)

var urlRegex = regexp.MustCompile(`https?://[^\s\n\r"'<>()\[\]{}]+[^\s\n\r"'<>()\[\]{}.,:;!?]`)

const (
	// Charcoal — used for all headings. One color, clear hierarchy via size.
	charcoal = uint32(0x2d3748)
	// Slate — body-level labels (Parameters:, GitHub:)
	slate = uint32(0x4a5568)
	// Code text — near-black, easy on the eyes
	codeText = uint32(0x3d3d3d)
	// Code background — barely-there gray
	codeBg = uint32(0xf7f8fa)
)

var sectionHeaders = map[string]bool{
	"PREREQUISITES":         true,
	"SETUP":                 true,
	"REPOSITORY STRUCTURE":  true,
	"ENVIRONMENT VARIABLES": true,
	"AVAILABLE TOOLS":       true,
	"UTILITIES":             true,
	"NOTES":                 true,
}

var toolNames = map[string]bool{
	"get_document":                       true,
	"create_document":                    true,
	"append_text":                        true,
	"insert_text":                        true,
	"replace_text":                       true,
	"delete_content_range":               true,
	"list_documents":                     true,
	"search_documents":                   true,
	"cmd/docformat — Document Formatter": true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: reformat <document-id-or-url>")
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
		fmt.Fprintf(os.Stderr, "service error: %v\n", err)
		os.Exit(1)
	}

	doc, err := svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		fmt.Fprintf(os.Stderr, "get doc error: %v\n", err)
		os.Exit(1)
	}

	// Pass 1: reset all text styles to clean defaults.
	resetReqs := buildResetRequests(doc)
	if len(resetReqs) > 0 {
		if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
			Requests: resetReqs,
		}).Context(ctx).Do(); err != nil {
			fmt.Fprintf(os.Stderr, "reset error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Reset %d paragraphs.\n", len(resetReqs)/2)
	}

	// Pass 2: apply clean formatting.
	doc, err = svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		fmt.Fprintf(os.Stderr, "re-read error: %v\n", err)
		os.Exit(1)
	}

	fmtReqs, deleteReqs := buildFormatRequests(doc)

	if len(fmtReqs) > 0 {
		if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
			Requests: fmtReqs,
		}).Context(ctx).Do(); err != nil {
			fmt.Fprintf(os.Stderr, "format error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Applied %d formatting operations.\n", len(fmtReqs))
	}

	if len(deleteReqs) > 0 {
		sort.Slice(deleteReqs, func(i, j int) bool {
			return deleteReqs[i].DeleteContentRange.Range.StartIndex >
				deleteReqs[j].DeleteContentRange.Range.StartIndex
		})
		if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
			Requests: deleteReqs,
		}).Context(ctx).Do(); err != nil {
			fmt.Fprintf(os.Stderr, "delete error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed %d separator lines.\n", len(deleteReqs))
	}

	// Pass 3: detect URLs and turn them into hyperlinks.
	doc, err = svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		fmt.Fprintf(os.Stderr, "re-read error: %v\n", err)
		os.Exit(1)
	}

	linkReqs := buildLinkRequests(doc)
	if len(linkReqs) > 0 {
		if _, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
			Requests: linkReqs,
		}).Context(ctx).Do(); err != nil {
			fmt.Fprintf(os.Stderr, "link error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Linked %d URL(s).\n", len(linkReqs))
	}

	fmt.Println("Done.")
}

// buildResetRequests strips all custom text styling from every paragraph so
// we start from a clean slate before applying the new look.
func buildResetRequests(doc *docs.Document) []*docs.Request {
	var reqs []*docs.Request
	for _, elem := range doc.Body.Content {
		if elem.Paragraph == nil {
			continue
		}
		start, end := elem.StartIndex, elem.EndIndex
		if end <= start+1 {
			continue
		}
		reqs = append(reqs,
			paraStyleReq(start, end,
				&docs.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
				"namedStyleType",
			),
			&docs.Request{
				UpdateTextStyle: &docs.UpdateTextStyleRequest{
					Range: &docs.Range{StartIndex: start, EndIndex: end - 1},
					TextStyle: &docs.TextStyle{
						Bold:               false,
						Italic:             false,
						ForegroundColor:    &docs.OptionalColor{},
						BackgroundColor:    &docs.OptionalColor{},
						WeightedFontFamily: &docs.WeightedFontFamily{FontFamily: "Arial"},
						FontSize:           pt(11),
					},
					Fields: "bold,italic,foregroundColor,backgroundColor,weightedFontFamily,fontSize",
				},
			},
		)
	}
	return reqs
}

func buildFormatRequests(doc *docs.Document) (fmtReqs, deleteReqs []*docs.Request) {
	for _, elem := range doc.Body.Content {
		if elem.Paragraph == nil {
			continue
		}
		text := strings.TrimRight(paragraphText(elem.Paragraph), "\n")
		start, end := elem.StartIndex, elem.EndIndex

		switch {

		case text == "Google Docs MCP Server — Usage Guide":
			fmtReqs = append(fmtReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{
					NamedStyleType: "HEADING_1",
					Alignment:      "CENTER",
					SpaceAbove:     pt(6),
					SpaceBelow:     pt(12),
				}, "namedStyleType,alignment,spaceAbove,spaceBelow"),
				textStyleReq(start, end-1, &docs.TextStyle{
					FontSize:        pt(22),
					Bold:            true,
					ForegroundColor: hexColor(charcoal),
				}, "fontSize,bold,foregroundColor"),
			)

		case strings.HasPrefix(text, "────"):
			deleteReqs = append(deleteReqs, &docs.Request{
				DeleteContentRange: &docs.DeleteContentRangeRequest{
					Range: &docs.Range{StartIndex: start, EndIndex: end},
				},
			})

		case sectionHeaders[text]:
			fmtReqs = append(fmtReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{
					NamedStyleType: "HEADING_2",
					SpaceAbove:     pt(18),
					SpaceBelow:     pt(4),
				}, "namedStyleType,spaceAbove,spaceBelow"),
				textStyleReq(start, end-1, &docs.TextStyle{
					FontSize:        pt(14),
					Bold:            true,
					ForegroundColor: hexColor(charcoal),
				}, "fontSize,bold,foregroundColor"),
			)

		case strings.HasPrefix(text, "Step "):
			fmtReqs = append(fmtReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{
					NamedStyleType: "HEADING_3",
					SpaceAbove:     pt(12),
					SpaceBelow:     pt(2),
				}, "namedStyleType,spaceAbove,spaceBelow"),
				textStyleReq(start, end-1, &docs.TextStyle{
					FontSize:        pt(12),
					Bold:            true,
					ForegroundColor: hexColor(charcoal),
				}, "fontSize,bold,foregroundColor"),
			)

		case toolNames[text]:
			fmtReqs = append(fmtReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{
					NamedStyleType: "HEADING_3",
					SpaceAbove:     pt(14),
					SpaceBelow:     pt(2),
				}, "namedStyleType,spaceAbove,spaceBelow"),
				textStyleReq(start, end-1, &docs.TextStyle{
					WeightedFontFamily: mono(),
					FontSize:           pt(11),
					Bold:               true,
					ForegroundColor:    hexColor(charcoal),
				}, "weightedFontFamily,fontSize,bold,foregroundColor"),
			)

		case isCodeBlock(text):
			fmtReqs = append(fmtReqs,
				paraStyleReq(start, end, &docs.ParagraphStyle{
					Shading:     &docs.Shading{BackgroundColor: hexColor(codeBg)},
					IndentStart: pt(16),
					SpaceAbove:  pt(2),
					SpaceBelow:  pt(2),
				}, "shading,indentStart,spaceAbove,spaceBelow"),
				textStyleReq(start, end-1, &docs.TextStyle{
					WeightedFontFamily: mono(),
					FontSize:           pt(10),
					ForegroundColor:    hexColor(codeText),
					BackgroundColor:    hexColor(codeBg),
				}, "weightedFontFamily,fontSize,foregroundColor,backgroundColor"),
			)

		case strings.TrimSpace(text) == "Parameters:":
			fmtReqs = append(fmtReqs,
				textStyleReq(start, end-1, &docs.TextStyle{
					Bold:            true,
					ForegroundColor: hexColor(slate),
					FontSize:        pt(10),
				}, "bold,foregroundColor,fontSize"),
			)

		case strings.HasPrefix(text, "GitHub:"):
			fmtReqs = append(fmtReqs,
				textStyleReq(start, start+7, &docs.TextStyle{
					Bold:            true,
					ForegroundColor: hexColor(slate),
				}, "bold,foregroundColor"),
			)
		}
	}
	return
}

func isCodeBlock(text string) bool {
	if strings.HasPrefix(text, "GOOGLE_") {
		return true
	}
	if !strings.HasPrefix(text, "   ") {
		return false
	}
	notCode := []string{
		"(required)", "(optional)", "Or set", "Parameters",
		"Read the", "Create a", "Append text", "Insert text",
		"Replace all", "Delete a", "Delete content", "List ", "Search for",
		"This opens", "Service account", "Restart Claude",
		"Beautify", "Code snippets", "Authentication is",
	}
	trimmed := strings.TrimSpace(text)
	for _, p := range notCode {
		if strings.Contains(trimmed, p) {
			return false
		}
	}
	codeSignals := []string{
		"cd ", "go ", "./", "export ", "{", "}",
		"\"command\"", "\"mcpServers\"", "\"google-docs\"", "\"env\"",
		"/path/to", "/absolute/", "├──", "│", "└──",
		"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_OAUTH_CREDENTIALS",
		"~/.config/", "claude_desktop_config.json",
	}
	for _, s := range codeSignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// ── Link detection ────────────────────────────────────────────────────────────

// buildLinkRequests scans every text run in the document, finds URLs via regex,
// and returns UpdateTextStyle requests that turn each one into a hyperlink.
func buildLinkRequests(doc *docs.Document) []*docs.Request {
	var reqs []*docs.Request
	for _, elem := range doc.Body.Content {
		if elem.Paragraph == nil {
			continue
		}
		for _, pe := range elem.Paragraph.Elements {
			if pe.TextRun == nil {
				continue
			}
			content := pe.TextRun.Content
			matches := urlRegex.FindAllStringIndex(content, -1)
			for _, m := range matches {
				url := content[m[0]:m[1]]
				// Google Docs API indices are UTF-16 code units.
				// For ASCII URLs, rune count == UTF-16 unit count.
				startOffset := int64(len([]rune(content[:m[0]])))
				endOffset := int64(len([]rune(content[:m[1]])))
				absStart := pe.StartIndex + startOffset
				absEnd := pe.StartIndex + endOffset
				reqs = append(reqs, &docs.Request{
					UpdateTextStyle: &docs.UpdateTextStyleRequest{
						Range: &docs.Range{StartIndex: absStart, EndIndex: absEnd},
						TextStyle: &docs.TextStyle{
							Link: &docs.Link{Url: url},
						},
						Fields: "link",
					},
				})
			}
		}
	}
	return reqs
}

// ── helpers ───────────────────────────────────────────────────────────────────

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

func hexColor(hex uint32) *docs.OptionalColor {
	return &docs.OptionalColor{Color: &docs.Color{RgbColor: &docs.RgbColor{
		Red:   float64((hex>>16)&0xff) / 255,
		Green: float64((hex>>8)&0xff) / 255,
		Blue:  float64(hex&0xff) / 255,
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
