package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"altalune.id/yasaku/internal/platform/config"
)

func explicitURL(cmd *cobra.Command) string {
	if f := cmd.Root().PersistentFlags().Lookup("url"); f != nil && f.Changed {
		return strings.TrimSpace(f.Value.String())
	}
	return strings.TrimSpace(os.Getenv(config.EnvVar("url")))
}

func sameURL(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

func registerURLFlagCompletion(root *cobra.Command) {
	_ = root.RegisterFlagCompletionFunc("url", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
}
