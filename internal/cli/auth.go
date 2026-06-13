package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const notionPATURL = "https://www.notion.so/developers/tokens"

func newAuthCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Notion credentials",
	}

	cmd.AddCommand(newAuthLoginCommand(opts))
	cmd.AddCommand(newAuthStatusCommand(opts))
	cmd.AddCommand(newAuthLogoutCommand(opts))

	return cmd
}

func newAuthLoginCommand(opts *options) *cobra.Command {
	var token string
	var noVerify bool
	var openBrowser bool
	var plaintextOK bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a Notion Personal Access Token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(token) == "" {
				printTokenInstructions(cmd.ErrOrStderr())

				if openBrowser {
					if err := openURL(notionPATURL); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Could not open browser automatically: %v\n", err)
					}
				}
			}

			if strings.TrimSpace(token) == "" {
				var err error
				token, err = readSecret(cmd.InOrStdin(), cmd.ErrOrStderr(), "Paste Notion Personal Access Token: ")
				if err != nil {
					return err
				}
			}

			token = strings.TrimSpace(token)
			if token == "" {
				return errors.New("token is required")
			}

			if !looksLikeNotionToken(token) {
				fmt.Fprintln(cmd.ErrOrStderr(), "Token does not look like a Notion PAT or integration token; expected prefix ntn_ or secret_.")
				ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), "Continue anyway? [y/N] ")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("login cancelled")
				}
			}

			if !noVerify {
				ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
				defer cancel()

				user, err := newNotionClient(opts, token).Verify(ctx)
				if err != nil {
					return fmt.Errorf("token verification failed: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Verified user: %s (%s)\n", user.DisplayName(), user.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "Token subject: %s\n", user.SubjectType())
				warnIfBotToken(cmd.ErrOrStderr(), user)
			}

			store := newStore(opts)
			if err := store.SaveKeychain(token); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %q to system keychain.\n", opts.profile)
				return nil
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "System keychain unavailable: %v\n", err)
			}

			if !plaintextOK {
				ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), "Store token in a plaintext file instead? [y/N] ")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("login cancelled; no credentials were saved")
				}
			}

			path, err := store.SavePlaintext(token)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %q to plaintext auth file: %s\n", opts.profile, path)
			return nil
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "Notion token to store; if omitted, prompt securely")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "skip verifying the token with Notion")
	cmd.Flags().BoolVar(&openBrowser, "open", false, "open the Notion integrations page before prompting")
	cmd.Flags().BoolVar(&plaintextOK, "plaintext-ok", false, "allow plaintext token storage if keychain is unavailable")

	return cmd
}

func newAuthStatusCommand(opts *options) *cobra.Command {
	var verify bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := newStore(opts)
			cred, err := store.Resolve()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Authenticated profile %q via %s.\n", opts.profile, cred.Source)

			if verify {
				ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
				defer cancel()

				user, err := newNotionClient(opts, cred.Token).Verify(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Verified user: %s (%s)\n", user.DisplayName(), user.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "Token subject: %s\n", user.SubjectType())
				warnIfBotToken(cmd.ErrOrStderr(), user)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&verify, "verify", false, "verify token against Notion")
	return cmd
}

func newAuthLogoutCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored Notion credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := newStore(opts)
			removed, err := store.DeleteAll()
			if err != nil {
				return err
			}

			if removed == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No stored credentials found for profile %q.\n", opts.profile)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed %d credential store(s) for profile %q.\n", removed, opts.profile)
			return nil
		},
	}
}

func warnIfBotToken(out io.Writer, user interface{ IsWorkspaceBot() bool }) {
	if !user.IsWorkspaceBot() {
		return
	}
	fmt.Fprintln(out, "Warning: this token appears to be a workspace-owned integration token, not a user Personal Access Token.")
	fmt.Fprintln(out, "Workspace-owned integration tokens only see pages/databases shared with the integration. For whole-workspace export, create a PAT at https://www.notion.so/developers/tokens and run auth login again.")
}

func printTokenInstructions(out io.Writer) {
	fmt.Fprintln(out, "Create or copy a Notion Personal Access Token:")
	fmt.Fprintf(out, "  %s\n", notionPATURL)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "When creating the token:")
	fmt.Fprintln(out, "  - Choose the workspace you want to export.")
	fmt.Fprintln(out, "  - Select Notion API access.")
	fmt.Fprintln(out, "  - Read content is required for export; Read comments is only needed if comment export is enabled later.")
	fmt.Fprintln(out, "  - Copy the token value. It should usually start with ntn_.")
	fmt.Fprintln(out, "")
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func readSecret(in io.Reader, errOut io.Writer, prompt string) (string, error) {
	stdinFile, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(stdinFile.Fd())) {
		data, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}

	fmt.Fprint(errOut, prompt)
	data, err := term.ReadPassword(int(stdinFile.Fd()))
	fmt.Fprintln(errOut)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func confirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func looksLikeNotionToken(token string) bool {
	return strings.HasPrefix(token, "ntn_") || strings.HasPrefix(token, "secret_")
}
