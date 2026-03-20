# Google Docs MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) server written in Go that gives Claude (and other MCP clients) full read/write access to Google Docs and Google Drive.

## Tools

| Tool | Description |
|------|-------------|
| `get_document` | Read the full text content of a Google Doc |
| `create_document` | Create a new blank Google Doc |
| `append_text` | Append text to the end of a document |
| `insert_text` | Insert text at a specific character index |
| `replace_text` | Replace all occurrences of a string |
| `delete_content_range` | Delete content between two character indices |
| `list_documents` | List recent Google Docs from Drive |
| `search_documents` | Search for documents by name |

## Prerequisites

- Go 1.22+
- A Google Cloud project with the **Google Docs API** and **Google Drive API** enabled

## Setup

### 1. Enable APIs and create credentials

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create or select a project
3. Enable **Google Docs API** and **Google Drive API**
4. Go to **APIs & Services → Credentials → Create Credentials → OAuth client ID**
5. Choose **Desktop application**, download the JSON file
6. Save it to `~/.config/google-docs-mcp/credentials.json`

   Or set the environment variable:
   ```
   export GOOGLE_OAUTH_CREDENTIALS=/path/to/your/credentials.json
   ```

### 2. Build

```bash
cd google-docs-mcp
go mod tidy
go build -o google-docs-mcp .
```

### 3. Authenticate

```bash
./google-docs-mcp --auth
```

This opens a browser window for OAuth consent. After approval the token is saved to `~/.config/google-docs-mcp/token.json` and reused automatically (refreshed when expired).

**Service account alternative:** Set `GOOGLE_APPLICATION_CREDENTIALS` to a service account key file — no interactive auth needed.

### 4. Add to Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "google-docs": {
      "command": "/absolute/path/to/google-docs-mcp"
    }
  }
}
```

Or with a service account:

```json
{
  "mcpServers": {
    "google-docs": {
      "command": "/absolute/path/to/google-docs-mcp",
      "env": {
        "GOOGLE_APPLICATION_CREDENTIALS": "/path/to/service-account.json"
      }
    }
  }
}
```

Restart Claude Desktop after editing the config.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to a service account key JSON file |
| `GOOGLE_OAUTH_CREDENTIALS` | Path to OAuth2 client credentials JSON (default: `~/.config/google-docs-mcp/credentials.json`) |

## Notes

- Document IDs and full `docs.google.com` URLs are both accepted wherever a `document_id` is required.
- Character indices in the Docs API are 1-based. Use `get_document` first to understand the document structure before inserting or deleting by index.
- `delete_content_range` uses an exclusive end index (same convention as the Docs API).
