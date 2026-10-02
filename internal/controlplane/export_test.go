package controlplane

import (
	"net/http"

	blogv1connect "altalune.id/yasaku/gen/go/blog/v1/blogv1connect"
	todov1connect "altalune.id/yasaku/gen/go/todo/v1/todov1connect"
)

// ReferenceHandler serves the blog and todo handlers, which yasaku does not mount, behind the production interceptor chain.
func (s *Server) ReferenceHandler(todos *TodoService, posts *BlogService) http.Handler {
	opts := s.handlerOptions()
	inner := http.NewServeMux()
	todoPath, todoHandler := todov1connect.NewTodoServiceHandler(todos, opts...)
	inner.Handle(todoPath, todoHandler)
	blogPath, blogHandler := blogv1connect.NewBlogServiceHandler(posts, opts...)
	inner.Handle(blogPath, blogHandler)
	return http.StripPrefix("/api", inner)
}
