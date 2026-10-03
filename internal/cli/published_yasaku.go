package cli

import (
	"github.com/spf13/cobra"

	"altalune.id/yasaku/internal/published"
)

func publishedDomainCmds(bootServer ServerBootFn, bootClient ClientBootFn) []*cobra.Command {
	var cmds []*cobra.Command
	if published.Todo {
		cmds = append(cmds, newTodoCmd(bootServer, bootClient))
	}
	if published.Blog {
		cmds = append(cmds, newBlogCmd())
	}
	return cmds
}
