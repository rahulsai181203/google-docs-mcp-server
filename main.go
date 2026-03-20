package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	docs "google.golang.org/api/docs/v1"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()

	// Handle auth setup flow
	if len(os.Args) > 1 && os.Args[1] == "--auth" {
		if err := runAuthSetup(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Authentication successful! You can now start the MCP server.")
		return
	}

	// Initialize Google API clients
	httpClient, err := newGoogleClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to authenticate: %v\n\nRun with --auth to set up authentication.\n", err)
		os.Exit(1)
	}

	docsService, err := docs.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Docs service: %v\n", err)
		os.Exit(1)
	}

	driveService, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Drive service: %v\n", err)
		os.Exit(1)
	}

	// Create MCP server
	s := server.NewMCPServer("Google Docs MCP", "1.0.0")

	// Register all tools
	registerAllTools(s, docsService, driveService)

	// Serve over stdio (MCP standard transport)
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func registerAllTools(s *server.MCPServer, docsService *docs.Service, driveService *drive.Service) {
	// get_document
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

	// create_document
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

	// append_text
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

	// insert_text
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

	// replace_text
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

	// delete_content_range
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

	// list_documents
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

	// search_documents
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
