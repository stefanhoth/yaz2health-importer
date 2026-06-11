package health

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// installedCreds mirrors the "installed" section of a Desktop-app
// client_secret.json. Google sometimes omits redirect_uris from the download;
// we override RedirectURL in Login() anyway, so we parse manually to avoid
// the strict validation in google.ConfigFromJSON.
type installedCreds struct {
	Installed struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"installed"`
}

// LoadOAuthConfig parses a Google "Desktop app" client_secret.json.
func LoadOAuthConfig(clientSecretPath string) (*oauth2.Config, error) {
	data, err := os.ReadFile(clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("read client secret: %w", err)
	}
	var c installedCreds
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse client secret: %w", err)
	}
	if c.Installed.ClientID == "" {
		return nil, fmt.Errorf("client secret: missing client_id — expected a Desktop app (\"installed\") credential file")
	}
	return &oauth2.Config{
		ClientID:     c.Installed.ClientID,
		ClientSecret: c.Installed.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       Scopes,
		// RedirectURL is set dynamically to the loopback listener in Login().
		RedirectURL: "http://localhost",
	}, nil
}

// Login runs the OAuth desktop flow: it starts a loopback listener, opens
// the consent page in the browser, exchanges the returned code, and stores
// the token at tokenPath (0600).
func Login(ctx context.Context, cfg *oauth2.Config, tokenPath string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()
	cfg.RedirectURL = fmt.Sprintf("http://%s/", listener.Addr().String())

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return err
	}
	state := hex.EncodeToString(stateBytes)

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			results <- result{err: errors.New("oauth state mismatch")}
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			results <- result{err: fmt.Errorf("oauth error: %s", errMsg)}
			return
		}
		fmt.Fprintln(w, "yaz2health: Anmeldung erfolgreich. Dieses Fenster kann geschlossen werden.")
		results <- result{code: r.URL.Query().Get("code")}
	})}
	go server.Serve(listener)
	defer server.Close()

	// AccessTypeOffline + prompt=consent force a refresh token even if the
	// user authorized this client before — the cron job depends on it.
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	fmt.Println("Öffne Browser für die Google-Anmeldung...")
	fmt.Println("Falls sich kein Browser öffnet, diese URL manuell öffnen:")
	fmt.Println(authURL)
	_ = exec.Command("open", authURL).Start()

	var res result
	select {
	case res = <-results:
	case <-time.After(5 * time.Minute):
		return errors.New("oauth login timed out after 5 minutes")
	case <-ctx.Done():
		return ctx.Err()
	}
	if res.err != nil {
		return res.err
	}

	token, err := cfg.Exchange(ctx, res.code)
	if err != nil {
		return fmt.Errorf("exchange oauth code: %w", err)
	}
	if token.RefreshToken == "" {
		return errors.New("google did not return a refresh token; remove the app's access at https://myaccount.google.com/permissions and retry")
	}
	return saveToken(tokenPath, token)
}

// TokenSource returns an auto-refreshing token source that persists
// refreshed tokens back to tokenPath, so the cron job keeps working across
// access token expiries.
func TokenSource(ctx context.Context, cfg *oauth2.Config, tokenPath string) (oauth2.TokenSource, error) {
	token, err := readToken(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("not logged in (run `yaz2health auth login`): %w", err)
	}
	return &savingTokenSource{
		src:  cfg.TokenSource(ctx, token),
		path: tokenPath,
		last: token,
	}, nil
}

type savingTokenSource struct {
	src  oauth2.TokenSource
	path string
	last *oauth2.Token
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if token.AccessToken != s.last.AccessToken {
		s.last = token
		if err := saveToken(s.path, token); err != nil {
			return nil, fmt.Errorf("persist refreshed token: %w", err)
		}
	}
	return token, nil
}

func readToken(path string) (*oauth2.Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &token, nil
}

func saveToken(path string, token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
