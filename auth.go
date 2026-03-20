package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OAuth2 scopes required by this server.
var oauthScopes = []string{
	"https://www.googleapis.com/auth/documents",
	"https://www.googleapis.com/auth/drive",
}

// newGoogleClient returns an authenticated HTTP client.
// It prefers service account credentials (GOOGLE_APPLICATION_CREDENTIALS env var),
// falling back to OAuth2 with a saved token.
func newGoogleClient(ctx context.Context) (*http.Client, error) {
	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		b, err := os.ReadFile(credFile)
		if err != nil {
			return nil, fmt.Errorf("reading service account file %s: %w", credFile, err)
		}
		creds, err := google.CredentialsFromJSON(ctx, b, oauthScopes...)
		if err != nil {
			return nil, fmt.Errorf("parsing service account credentials: %w", err)
		}
		return oauth2.NewClient(ctx, creds.TokenSource), nil
	}

	config, err := loadOAuthConfig()
	if err != nil {
		return nil, err
	}

	token, err := loadSavedToken(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%w\nRun: google-docs-mcp --auth", err)
	}

	return config.Client(ctx, token), nil
}

// runAuthSetup runs the interactive OAuth2 flow and saves the token.
func runAuthSetup(ctx context.Context) error {
	config, err := loadOAuthConfig()
	if err != nil {
		return err
	}
	token, err := runOAuthFlow(ctx, config)
	if err != nil {
		return err
	}
	return saveToken(token)
}

func loadOAuthConfig() (*oauth2.Config, error) {
	credsPath := oauthCredsPath()
	b, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, fmt.Errorf(
			"OAuth credentials not found at %s\n\n"+
				"Setup steps:\n"+
				"  1. Go to https://console.cloud.google.com/\n"+
				"  2. Enable the Google Docs API and Google Drive API\n"+
				"  3. Create OAuth 2.0 credentials (Desktop application)\n"+
				"  4. Download as credentials.json and place at: %s\n"+
				"  5. Run: google-docs-mcp --auth",
			credsPath, credsPath,
		)
	}
	config, err := google.ConfigFromJSON(b, oauthScopes...)
	if err != nil {
		return nil, fmt.Errorf("parsing OAuth credentials: %w", err)
	}
	return config, nil
}

func loadSavedToken(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	tokenPath := tokenFilePath()
	b, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("no saved token found at %s", tokenPath)
	}

	var token oauth2.Token
	if err := json.Unmarshal(b, &token); err != nil {
		return nil, fmt.Errorf("invalid token file: %w", err)
	}

	// Refresh if expired
	ts := config.TokenSource(ctx, &token)
	refreshed, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("token refresh failed (re-run --auth): %w", err)
	}

	// Persist refreshed token
	if refreshed.AccessToken != token.AccessToken {
		_ = saveToken(refreshed)
	}

	return refreshed, nil
}

func runOAuthFlow(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Addr: ":8085", Handler: mux}

	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if e := r.URL.Query().Get("error"); e != "" {
			fmt.Fprintf(w, "Authentication failed: %s", e)
			errCh <- fmt.Errorf("oauth error: %s", e)
			return
		}
		code := r.URL.Query().Get("code")
		fmt.Fprintln(w, "<html><body><h2>Authentication successful!</h2><p>You can close this tab.</p></body></html>")
		codeCh <- code
	})

	config.RedirectURL = "http://localhost:8085/oauth/callback"
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	defer srv.Shutdown(shutdownCtx) //nolint:errcheck

	fmt.Fprintf(os.Stderr, "\n=== Google Docs MCP — Authentication ===\n\n")
	fmt.Fprintf(os.Stderr, "Opening Chrome for authentication...\n")

	// Open in Chrome; fall back to the system default browser if Chrome is not found.
	if err := exec.Command("open", "-a", "Google Chrome", authURL).Start(); err != nil {
		if err2 := exec.Command("open", authURL).Start(); err2 != nil {
			fmt.Fprintf(os.Stderr, "Could not open browser automatically.\nOpen this URL manually:\n\n%s\n\n", authURL)
		}
	}

	fmt.Fprintf(os.Stderr, "Waiting for authentication (timeout: 5 min)...\n")

	select {
	case code := <-codeCh:
		token, err := config.Exchange(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("exchanging auth code: %w", err)
		}
		return token, nil
	case err := <-errCh:
		return nil, err
	case <-shutdownCtx.Done():
		return nil, fmt.Errorf("authentication timed out")
	}
}

func saveToken(token *oauth2.Token) error {
	path := tokenFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	b, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}
	return os.WriteFile(path, b, 0600)
}

func oauthCredsPath() string {
	if p := os.Getenv("GOOGLE_OAUTH_CREDENTIALS"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "google-docs-mcp", "credentials.json")
}

func tokenFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "google-docs-mcp", "token.json")
}
