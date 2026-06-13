package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spashii/notion-export-cli/internal/exporter"
	"github.com/spf13/cobra"
)

func newExportCommand(opts *options) *cobra.Command {
	var all bool
	var clean bool
	var outputDir string

	cmd := &cobra.Command{
		Use:   "export [page-or-database-url-or-id] [output-dir]",
		Short: "Export Notion content to a local folder",
		Long: `Export a Notion page, database, or data source into a local folder.

Pages are written as Markdown files. Pages with child pages/databases become
folders with an index.md. Databases and data sources become folders with JSON
sidecars plus one Markdown file/folder per row.

If output-dir is omitted, content is written to ./notion-out.

Use --all to export all accessible root pages and data sources discovered via
Notion search. For Personal Access Tokens this means content accessible to the
token's user in that workspace.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) > 1 {
					return fmt.Errorf("usage with --all: notion-export export --all [output-dir]")
				}
				return nil
			}
			if len(args) < 1 || len(args) > 2 {
				return fmt.Errorf("usage: notion-export export [page-or-database-url-or-id] [output-dir]")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			store := newStore(opts)
			cred, err := store.Resolve()
			if err != nil {
				return err
			}

			resolvedOutputDir := outputDir
			if all {
				if len(args) == 1 {
					resolvedOutputDir = args[0]
				}
			} else if len(args) == 2 {
				resolvedOutputDir = args[1]
			}
			if clean {
				if err := cleanOutputDir(resolvedOutputDir); err != nil {
					return err
				}
			}

			exporter := exporter.New(exporter.Config{
				Client:    newNotionClient(opts, cred.Token),
				OutputDir: resolvedOutputDir,
				Writer:    cmd.OutOrStdout(),
			})

			if all {
				return exporter.ExportAll(cmd.Context())
			}
			return exporter.ExportRoot(cmd.Context(), args[0])
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "export all accessible root pages and data sources")
	cmd.Flags().BoolVar(&clean, "clean", false, "remove the output directory before exporting")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "notion-out", "output directory when omitted as a positional argument")
	return cmd
}

func cleanOutputDir(path string) error {
	path = filepath.Clean(path)
	if path == "" || path == "." || path == string(os.PathSeparator) {
		return fmt.Errorf("--clean refuses to remove unsafe output directory %q", path)
	}
	return os.RemoveAll(path)
}
