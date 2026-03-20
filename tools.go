package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	docs "google.golang.org/api/docs/v1"
	drive "google.golang.org/api/drive/v3"
)

func registerTools(s *server.MCPServer, docsService *docs.Service, driveService *drive.Service) {
	s.AddTool(
		mcp.NewTool("get_document",
			mcp.WithDescription("Get the full text content and metadata of a Google Doc"),
			mcp.WithString("document_id",
				mcp.Required(),
				mcp.Description("The Google Doc ID or full URL (e.g. https://docs.google.com/document/d/DOC_ID/edit)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			docID, err := req.RequireString("document_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return handleGetDocument(ctx, docsService, extractDocID(docID))
		},
	)

	s.AddTool(
		mcp.NewTool("create_document",
			mcp.WithDescription("Create a new blank Google Doc with a given title"),
			mcp.WithString("title",
				mcp.Required(),
				mcp.Description("Title of the new document"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			title, err := req.RequireString("title")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return handleCreateDocument(ctx, docsService, title)
		},
	)

	s.AddTool(
		mcp.NewTool("append_text",
			mcp.WithDescription("Append text to the end of a Google Doc"),
			mcp.WithString("document_id",
				mcp.Required(),
				mcp.Description("The Google Doc ID or URL"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to append (use \\n for newlines)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			docID, err := req.RequireString("document_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			text, err := req.RequireString("text")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return handleAppendText(ctx, docsService, extractDocID(docID), text)
		},
	)

	s.AddTool(
		mcp.NewTool("insert_text",
			mcp.WithDescription("Insert text at a specific character index in a Google Doc"),
			mcp.WithString("document_id",
				mcp.Required(),
				mcp.Description("The Google Doc ID or URL"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to insert"),
			),
			mcp.WithNumber("index",
				mcp.Required(),
				mcp.Description("Character index at which to insert (1-based; use get_document to find indices)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			docID, err := req.RequireString("document_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			text, err := req.RequireString("text")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			index := int64(req.GetFloat("index", 1))
			return handleInsertText(ctx, docsService, extractDocID(docID), text, index)
		},
	)

	s.AddTool(
		mcp.NewTool("replace_text",
			mcp.WithDescription("Replace all occurrences of a string in a Google Doc"),
			mcp.WithString("document_id",
				mcp.Required(),
				mcp.Description("The Google Doc ID or URL"),
			),
			mcp.WithString("old_text",
				mcp.Required(),
				mcp.Description("Text to find and replace"),
			),
			mcp.WithString("new_text",
				mcp.Required(),
				mcp.Description("Replacement text"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			docID, err := req.RequireString("document_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			oldText, err := req.RequireString("old_text")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			newText, err := req.RequireString("new_text")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return handleReplaceText(ctx, docsService, extractDocID(docID), oldText, newText)
		},
	)

	s.AddTool(
		mcp.NewTool("delete_content_range",
			mcp.WithDescription("Delete a range of content from a Google Doc by start and end index"),
			mcp.WithString("document_id",
				mcp.Required(),
				mcp.Description("The Google Doc ID or URL"),
			),
			mcp.WithNumber("start_index",
				mcp.Required(),
				mcp.Description("Start index of content to delete (inclusive, 1-based)"),
			),
			mcp.WithNumber("end_index",
				mcp.Required(),
				mcp.Description("End index of content to delete (exclusive)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			docID, err := req.RequireString("document_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			startIndex := int64(req.GetFloat("start_index", 1))
			endIndex := int64(req.GetFloat("end_index", 1))
			return handleDeleteContentRange(ctx, docsService, extractDocID(docID), startIndex, endIndex)
		},
	)

	s.AddTool(
		mcp.NewTool("list_documents",
			mcp.WithDescription("List Google Docs from your Drive, sorted by most recently modified"),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum number of documents to return (default: 20, max: 100)"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			maxResults := int64(req.GetFloat("max_results", 20))
			if maxResults > 100 {
				maxResults = 100
			}
			return handleListDocuments(ctx, driveService, maxResults)
		},
	)

	s.AddTool(
		mcp.NewTool("search_documents",
			mcp.WithDescription("Search for Google Docs by name in your Drive"),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Search term to match against document names"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := req.RequireString("query")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return handleSearchDocuments(ctx, driveService, query)
		},
	)
}
