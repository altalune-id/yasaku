package boot

import (
	"slices"

	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/published"
	"altalune.id/yasaku/internal/todo"
	"altalune.id/yasaku/internal/web"
	webhandlers "altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/scheduler"
)

const (
	mountBlog            = published.Blog
	mountTodo            = published.Todo
	mountWebhooksConsole = published.WebhooksConsole
)

func publishedMCPDomains() []string { return []string{"yasaku.v1"} }

func publishedConsoleHandlers(all []web.Register) []web.Register {
	return slices.DeleteFunc(slices.Clone(all), func(h web.Register) bool {
		switch h.(type) {
		case *webhandlers.BlogHandler:
			return !mountBlog
		case *webhandlers.TodoHandler:
			return !mountTodo
		case *webhandlers.WebhookHandler:
			return !mountWebhooksConsole
		}
		return false
	})
}

// NOTE: yasakuConsoleHandlers is the hook for yasaku-only console modules; a module the server leaves unmounted adds nothing.
func yasakuConsoleHandlers(deps webhandlers.Deps, s *Services) []web.Register {
	var hs []web.Register
	if s.Opensheet != nil {
		hs = append(hs, webhandlers.NewOpensheetHandler(deps, s.Opensheet))
	}
	return hs
}

func publishedConsumers(all []queue.Provider) []queue.Provider {
	return slices.DeleteFunc(slices.Clone(all), func(p queue.Provider) bool {
		_, isTodo := p.(*todo.Consumer)
		return isTodo && !mountTodo
	})
}

func publishedSchedulers(all []scheduler.Provider) []scheduler.Provider {
	return slices.DeleteFunc(slices.Clone(all), func(p scheduler.Provider) bool {
		_, isTodo := p.(*todo.Scheduler)
		return isTodo && !mountTodo
	})
}
