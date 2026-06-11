// yaz2health syncs Yazio nutrition and hydration data into Google Health.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/api/option"

	"github.com/stefanhoth/yaz2health/internal/health"
	"github.com/stefanhoth/yaz2health/internal/syncer"
	"github.com/stefanhoth/yaz2health/internal/yazio"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func configDir() (string, error) {
	base, err := os.UserConfigDir() // macOS: ~/Library/Application Support
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "yaz2health"), nil
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "yaz2health",
		Short:         "Sync Yazio nutrition and hydration data into Google Health",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(authCmd(), syncCmd())
	return root
}

func authCmd() *cobra.Command {
	auth := &cobra.Command{Use: "auth", Short: "Google authentication helpers"}

	var clientSecret string
	login := &cobra.Command{
		Use:   "login",
		Short: "Run the OAuth flow and store the token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := configDir()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			// Keep a copy of the client secret so later runs need no flag.
			storedSecret := filepath.Join(dir, "client_secret.json")
			if clientSecret != "" {
				data, err := os.ReadFile(clientSecret)
				if err != nil {
					return fmt.Errorf("read client secret: %w", err)
				}
				if err := os.WriteFile(storedSecret, data, 0o600); err != nil {
					return err
				}
			}
			cfg, err := health.LoadOAuthConfig(storedSecret)
			if err != nil {
				return fmt.Errorf("%w (pass --client-secret on first login)", err)
			}
			if err := health.Login(cmd.Context(), cfg, filepath.Join(dir, "token.json")); err != nil {
				return err
			}
			fmt.Println("Login erfolgreich, Token gespeichert in", dir)
			return nil
		},
	}
	login.Flags().StringVar(&clientSecret, "client-secret", "", "Path to the Google OAuth client_secret.json (Desktop app)")

	status := &cobra.Command{
		Use:   "status",
		Short: "Show whether a Google token is stored",
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, err := configDir()
			if err != nil {
				return err
			}
			tokenPath := filepath.Join(dir, "token.json")
			cfg, err := health.LoadOAuthConfig(filepath.Join(dir, "client_secret.json"))
			if err != nil {
				fmt.Println("not logged in (no client secret stored)")
				return nil
			}
			src, err := health.TokenSource(context.Background(), cfg, tokenPath)
			if err != nil {
				fmt.Println("not logged in")
				return nil
			}
			token, err := src.Token()
			if err != nil {
				fmt.Println("token invalid or revoked:", err)
				return nil
			}
			fmt.Println("status: valid")
			fmt.Println("expires:", token.Expiry.Local().Format(time.RFC3339))
			fmt.Println("config:", dir)
			return nil
		},
	}

	auth.AddCommand(login, status)
	return auth
}

func syncCmd() *cobra.Command {
	var (
		from, to, timezone string
		days, lookback     int
		dryRun             bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync a date range from Yazio into Google Health",
		Long: `Sync a date range from Yazio into Google Health.

Default range: today (UTC, matching Yazio's day boundaries) plus the
lookback window. Use --days for backfills, or --from/--to for an
explicit range. All forms are idempotent: re-running never duplicates.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days > 0 && (from != "" || to != "") {
				return errors.New("--days and --from/--to are mutually exclusive")
			}
			// Yazio days are UTC-based, so "today" is too.
			today := time.Now().UTC()
			switch {
			case days > 0:
				from = today.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
				to = today.Format("2006-01-02")
			case from == "" && to == "":
				from = today.AddDate(0, 0, -lookback).Format("2006-01-02")
				to = today.Format("2006-01-02")
			case from == "" || to == "":
				return errors.New("--from and --to must be used together")
			}

			loc, err := time.LoadLocation(timezone)
			if err != nil {
				return fmt.Errorf("invalid timezone %q: %w", timezone, err)
			}

			dir, err := configDir()
			if err != nil {
				return err
			}
			cfg, err := health.LoadOAuthConfig(filepath.Join(dir, "client_secret.json"))
			if err != nil {
				return fmt.Errorf("%w (run `yaz2health auth login --client-secret ...` first)", err)
			}
			tokenSource, err := health.TokenSource(cmd.Context(), cfg, filepath.Join(dir, "token.json"))
			if err != nil {
				return err
			}
			sink, err := health.New(cmd.Context(), "me", loc, option.WithTokenSource(tokenSource))
			if err != nil {
				return err
			}

			s := &syncer.Syncer{
				Source:   &yazio.Client{},
				Sink:     sink,
				DryRun:   dryRun,
				Throttle: 300 * time.Millisecond,
				Out:      os.Stdout,
			}
			label := ""
			if dryRun {
				label = " (dry-run)"
			}
			fmt.Printf("Syncing %s..%s%s\n", from, to, label)
			stats, err := s.Run(cmd.Context(), from, to)
			if err != nil {
				return err
			}
			fmt.Println(stats)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Start date (YYYY-MM-DD, inclusive)")
	cmd.Flags().StringVar(&to, "to", "", "End date (YYYY-MM-DD, inclusive)")
	cmd.Flags().IntVar(&days, "days", 0, "Sync the last N days up to today (e.g. 30 for a backfill)")
	cmd.Flags().IntVar(&lookback, "lookback", 3, "Days before today to include in the default range")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show planned actions without writing")
	cmd.Flags().StringVar(&timezone, "tz", "Europe/Berlin", "Timezone for meal interval times in Google Health")
	return cmd
}
