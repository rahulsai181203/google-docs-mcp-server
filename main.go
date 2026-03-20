package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	docs "google.golang.org/api/docs/v1"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"google-docs-mcp/internal/auth"
)

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "--auth" {
		if err := auth.RunAuthSetup(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Authentication successful! You can now start the MCP server.")
		return
	}

	httpClient, err := auth.NewGoogleClient(ctx)
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

	s := server.NewMCPServer("Google Docs MCP", "1.0.0")
	registerTools(s, docsService, driveService)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
