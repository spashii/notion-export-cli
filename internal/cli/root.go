package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	tokenauth "github.com/spashii/notion-export-cli/internal/auth"
	"github.com/spashii/notion-export-cli/internal/notion"
	"github.com/spf13/cobra"
)

type options struct {
	profile       string
	notionVersion string
	apiBaseURL    string
	rps           float64
}

func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := &options{
		profile:       "default",
		notionVersion: notion.DefaultVersion,
		apiBaseURL:    notion.DefaultBaseURL,
		rps:           notion.DefaultRPS,
	}

	cmd := &cobra.Command{
		Use:           "notion-export",
		Short:         "Export Notion pages and databases to local files",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&opts.profile, "profile", opts.profile, "auth profile name")
	cmd.PersistentFlags().StringVar(&opts.notionVersion, "notion-version", opts.notionVersion, "Notion API version header")
	cmd.PersistentFlags().StringVar(&opts.apiBaseURL, "api-base-url", opts.apiBaseURL, "Notion API base URL")
	cmd.PersistentFlags().Float64Var(&opts.rps, "rps", opts.rps, "maximum average Notion API requests per second")

	cmd.AddCommand(newAuthCommand(opts))
	cmd.AddCommand(newDoctorCommand(opts))
	cmd.AddCommand(newExportCommand(opts))

	return cmd
}

func newStore(opts *options) tokenauth.Store {
	return tokenauth.NewStore("notion-export-cli", opts.profile)
}

func newNotionClient(opts *options, token string) *notion.Client {
	return notion.NewClient(notion.Config{
		Token:         token,
		BaseURL:       opts.apiBaseURL,
		NotionVersion: opts.notionVersion,
		RPS:           opts.rps,
	})
}

func newDoctorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify authentication and API connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := newStore(opts)
			cred, err := store.Resolve()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			user, err := newNotionClient(opts, cred.Token).Verify(ctx)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Auth source: %s\n", cred.Source)
			fmt.Fprintf(cmd.OutOrStdout(), "Profile: %s\n", opts.profile)
			fmt.Fprintf(cmd.OutOrStdout(), "Notion API: %s (%s)\n", opts.apiBaseURL, opts.notionVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Rate limit: %.2f req/s (Notion documents %.2f req/s average)\n", opts.rps, notion.DocumentedAverageRPS)
			fmt.Fprintf(cmd.OutOrStdout(), "Verified user: %s (%s)\n", user.DisplayName(), user.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Token subject: %s\n", user.SubjectType())
			warnIfBotToken(cmd.ErrOrStderr(), user)
			return nil
		},
	}
}
