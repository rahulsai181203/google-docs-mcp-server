package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	docs "google.golang.org/api/docs/v1"
	drive "google.golang.org/api/drive/v3"
)

// extractDocID parses a document ID from a full Google Docs URL or returns the
// input unchanged if it is already a bare ID.
func extractDocID(input string) string {
	const marker = "/document/d/"
	if idx := strings.Index(input, marker); idx != -1 {
		rest := input[idx+len(marker):]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			return rest[:slashIdx]
		}
		return rest
	}
	return input
}

// extractBodyText converts a Google Doc's body into plain text.
func extractBodyText(doc *docs.Document) string {
	if doc.Body == nil {
		return ""
	}
	var sb strings.Builder
	for _, elem := range doc.Body.Content {
		writeElement(&sb, elem)
	}
	return sb.String()
}

func writeElement(sb *strings.Builder, elem *docs.StructuralElement) {
	switch {
	case elem.Paragraph != nil:
		for _, pe := range elem.Paragraph.Elements {
			if pe.TextRun != nil {
				sb.WriteString(pe.TextRun.Content)
			}
		}
	case elem.Table != nil:
		for _, row := range elem.Table.TableRows {
			cells := make([]string, 0, len(row.TableCells))
			for _, cell := range row.TableCells {
				var cellSB strings.Builder
				for _, cellElem := range cell.Content {
					writeElement(&cellSB, cellElem)
				}
				cells = append(cells, strings.TrimRight(cellSB.String(), "\n"))
			}
			sb.WriteString(strings.Join(cells, " | "))
			sb.WriteString("\n")
		}
	case elem.SectionBreak != nil:
		sb.WriteString("\n")
	}
}

// ── Tool Handlers ─────────────────────────────────────────────────────────────

func handleGetDocument(ctx context.Context, svc *docs.Service, docID string) (*mcp.CallToolResult, error) {
	doc, err := svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get document: %v", err)), nil
	}

	text := extractBodyText(doc)
	result := fmt.Sprintf(
		"Title: %s\nID:    %s\nURL:   https://docs.google.com/document/d/%s/edit\n\n%s",
		doc.Title, doc.DocumentId, doc.DocumentId, text,
	)
	return mcp.NewToolResultText(result), nil
}

func handleCreateDocument(ctx context.Context, svc *docs.Service, title string) (*mcp.CallToolResult, error) {
	doc, err := svc.Documents.Create(&docs.Document{Title: title}).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create document: %v", err)), nil
	}

	result := fmt.Sprintf(
		"Document created successfully.\nTitle: %s\nID:    %s\nURL:   https://docs.google.com/document/d/%s/edit",
		doc.Title, doc.DocumentId, doc.DocumentId,
	)
	return mcp.NewToolResultText(result), nil
}

func handleAppendText(ctx context.Context, svc *docs.Service, docID, text string) (*mcp.CallToolResult, error) {
	doc, err := svc.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get document: %v", err)), nil
	}

	// Insert before the trailing newline that terminates the document body.
	endIndex := doc.Body.Content[len(doc.Body.Content)-1].EndIndex - 1

	_, err = svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{
			{
				InsertText: &docs.InsertTextRequest{
					Location: &docs.Location{Index: endIndex},
					Text:     text,
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to append text: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Appended %d character(s) to document %s.", len(text), docID)), nil
}

func handleInsertText(ctx context.Context, svc *docs.Service, docID, text string, index int64) (*mcp.CallToolResult, error) {
	_, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{
			{
				InsertText: &docs.InsertTextRequest{
					Location: &docs.Location{Index: index},
					Text:     text,
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to insert text at index %d: %v", index, err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Inserted %d character(s) at index %d.", len(text), index)), nil
}

func handleReplaceText(ctx context.Context, svc *docs.Service, docID, oldText, newText string) (*mcp.CallToolResult, error) {
	resp, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{
			{
				ReplaceAllText: &docs.ReplaceAllTextRequest{
					ContainsText: &docs.SubstringMatchCriteria{
						Text:      oldText,
						MatchCase: true,
					},
					ReplaceText: newText,
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to replace text: %v", err)), nil
	}

	count := int64(0)
	if len(resp.Replies) > 0 && resp.Replies[0].ReplaceAllText != nil {
		count = resp.Replies[0].ReplaceAllText.OccurrencesChanged
	}

	return mcp.NewToolResultText(fmt.Sprintf("Replaced %d occurrence(s) of %q with %q.", count, oldText, newText)), nil
}

func handleDeleteContentRange(ctx context.Context, svc *docs.Service, docID string, startIndex, endIndex int64) (*mcp.CallToolResult, error) {
	_, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{
			{
				DeleteContentRange: &docs.DeleteContentRangeRequest{
					Range: &docs.Range{
						StartIndex: startIndex,
						EndIndex:   endIndex,
					},
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete range [%d, %d): %v", startIndex, endIndex, err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Deleted content from index %d to %d.", startIndex, endIndex)), nil
}

func handleListDocuments(ctx context.Context, svc *drive.Service, maxResults int64) (*mcp.CallToolResult, error) {
	r, err := svc.Files.List().
		Q("mimeType='application/vnd.google-apps.document' and trashed=false").
		Fields("files(id, name, modifiedTime, webViewLink)").
		OrderBy("modifiedTime desc").
		PageSize(maxResults).
		Context(ctx).
		Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list documents: %v", err)), nil
	}

	if len(r.Files) == 0 {
		return mcp.NewToolResultText("No documents found."), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d document(s):\n\n", len(r.Files))
	for i, f := range r.Files {
		fmt.Fprintf(&sb, "%d. %s\n   ID:       %s\n   Modified: %s\n   URL:      %s\n\n",
			i+1, f.Name, f.Id, f.ModifiedTime, f.WebViewLink)
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func handleSearchDocuments(ctx context.Context, svc *drive.Service, query string) (*mcp.CallToolResult, error) {
	// Escape characters special to the Drive API query language.
	escaped := strings.ReplaceAll(query, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)

	r, err := svc.Files.List().
		Q(fmt.Sprintf("mimeType='application/vnd.google-apps.document' and name contains '%s' and trashed=false", escaped)).
		Fields("files(id, name, modifiedTime, webViewLink)").
		OrderBy("modifiedTime desc").
		PageSize(50).
		Context(ctx).
		Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to search documents: %v", err)), nil
	}

	if len(r.Files) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No documents found matching %q.", query)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d document(s) matching %q:\n\n", len(r.Files), query)
	for i, f := range r.Files {
		fmt.Fprintf(&sb, "%d. %s\n   ID:       %s\n   Modified: %s\n   URL:      %s\n\n",
			i+1, f.Name, f.Id, f.ModifiedTime, f.WebViewLink)
	}
	return mcp.NewToolResultText(sb.String()), nil
}
