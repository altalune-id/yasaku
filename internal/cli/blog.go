package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/cli/render"
	"altalune.id/yasaku/internal/dataplane"
)

func newBlogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "blog",
		Short:   "Manage blog posts over the REST data plane",
		Long:    "Manage blog posts over the REST data plane (S3), authenticated by API key. The CLI walks the same public path an integrator does.",
		GroupID: "domain",
	}
	cmd.AddCommand(
		newBlogListCmd(),
		newBlogGetCmd(),
		newBlogCreateCmd(),
		newBlogUpdateCmd(),
		newBlogPublishCmd(),
		newBlogUnpublishCmd(),
		newBlogDeleteCmd(),
	)
	return cmd
}

func newBlogListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List posts in the active project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target, err := blogTargetFrom(cmd)
			if err != nil {
				return err
			}
			posts, err := target.client.ListPosts(cmd.Context(), target.org, target.project)
			if err != nil {
				return blogError("blog list", err)
			}
			return renderPosts(cmd, posts)
		},
	}
}

func newBlogGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <slug>",
		Short: "Read one post by slug",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := blogTargetFrom(cmd)
			if err != nil {
				return err
			}
			post, err := target.client.GetPost(cmd.Context(), target.org, target.project, args[0])
			if err != nil {
				return blogError("blog get", err)
			}
			return renderPost(cmd, post)
		},
	}
}

func newBlogCreateCmd() *cobra.Command {
	var title, slug, body, category, idempotencyKey string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a post",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target, err := blogTargetFrom(cmd)
			if err != nil {
				return err
			}
			in := dataplane.PostInput{Title: &title, Slug: &slug, Body: &body, CategoryID: &category}
			post, err := target.client.CreatePost(cmd.Context(), target.org, target.project, in, dataplane.WriteOpts{
				IdempotencyKey: idempotencyKey,
			})
			if err != nil {
				return blogError("blog create", err)
			}
			return renderPost(cmd, post)
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "post title")
	f.StringVar(&slug, "slug", "", "post slug (unique within the project)")
	f.StringVar(&body, "body", "", "post body")
	f.StringVar(&category, "category", "", "category uuid the post belongs to")
	f.StringVar(&idempotencyKey, "idempotency-key", "", "make the create replay-safe; a retry with the same key and body returns the first result")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("slug")
	_ = cmd.MarkFlagRequired("category")
	return cmd
}

func newBlogUpdateCmd() *cobra.Command {
	var title, slug, body, category string
	var ifVersion int
	var replace bool
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update a post, guarded by its version",
		Long:  "Update a post. --if-version is the version last read with `yasaku blog get`; the data plane rejects the write when the post moved on.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := blogTargetFrom(cmd)
			if err != nil {
				return err
			}
			flags := cmd.Flags()
			in := dataplane.PostInput{}
			if flags.Changed("title") {
				in.Title = &title
			}
			if flags.Changed("slug") {
				in.Slug = &slug
			}
			if flags.Changed("body") {
				in.Body = &body
			}
			if flags.Changed("category") {
				in.CategoryID = &category
			}
			opts := dataplane.WriteOpts{IfVersion: ifVersion}
			write := target.client.PatchPost
			if replace {
				write = target.client.ReplacePost
			}
			post, err := write(cmd.Context(), target.org, target.project, args[0], in, opts)
			if err != nil {
				return blogError("blog update", err)
			}
			return renderPost(cmd, post)
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "new title")
	f.StringVar(&slug, "slug", "", "new slug")
	f.StringVar(&body, "body", "", "new body")
	f.StringVar(&category, "category", "", "new category uuid")
	f.IntVar(&ifVersion, "if-version", 0, "version the write is conditioned on, from `yasaku blog get`")
	f.BoolVar(&replace, "replace", false, "replace the post wholesale (PUT) instead of patching the given fields")
	_ = cmd.MarkFlagRequired("if-version")
	return cmd
}

func newBlogPublishCmd() *cobra.Command {
	return newBlogTransitionCmd(
		"publish <slug>",
		"Publish a post, guarded by its version",
		"Publish a post so it is readable without a credential where the instance enables public reads. --if-version is the version last read with `yasaku blog get`.",
		func(c *dataplane.Client) blogTransition { return c.PublishPost },
		"blog publish",
	)
}

func newBlogUnpublishCmd() *cobra.Command {
	return newBlogTransitionCmd(
		"unpublish <slug>",
		"Return a post to draft, guarded by its version",
		"Return a post to draft, so it is no longer readable without a credential. --if-version is the version last read with `yasaku blog get`.",
		func(c *dataplane.Client) blogTransition { return c.UnpublishPost },
		"blog unpublish",
	)
}

type blogTransition func(ctx context.Context, org, project, slug string, opts dataplane.WriteOpts) (dataplane.Post, error)

func newBlogTransitionCmd(use, short, long string, pick func(*dataplane.Client) blogTransition, op string) *cobra.Command {
	var ifVersion int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := blogTargetFrom(cmd)
			if err != nil {
				return err
			}
			post, err := pick(target.client)(cmd.Context(), target.org, target.project, args[0], dataplane.WriteOpts{
				IfVersion: ifVersion,
			})
			if err != nil {
				return blogError(op, err)
			}
			return renderPost(cmd, post)
		},
	}
	cmd.Flags().IntVar(&ifVersion, "if-version", 0, "version the transition is conditioned on, from `yasaku blog get`")
	_ = cmd.MarkFlagRequired("if-version")
	return cmd
}

func newBlogDeleteCmd() *cobra.Command {
	var ifVersion int
	cmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete a post, guarded by its version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := blogTargetFrom(cmd)
			if err != nil {
				return err
			}
			if err := target.client.DeletePost(cmd.Context(), target.org, target.project, args[0], dataplane.WriteOpts{
				IfVersion: ifVersion,
			}); err != nil {
				return blogError("blog delete", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted post %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().IntVar(&ifVersion, "if-version", 0, "version the delete is conditioned on, from `yasaku blog get`")
	_ = cmd.MarkFlagRequired("if-version")
	return cmd
}

type blogTarget struct {
	client  *dataplane.Client
	org     string
	project string
}

// SECURITY: the key always comes from credentialFor, never straight off the session file, so a credential saved for one instance is never offered to another.
func blogTargetFrom(cmd *cobra.Command) (blogTarget, error) {
	cfg, err := withCfg(cmd)
	if err != nil {
		return blogTarget{}, err
	}
	instance := resolveURL(cmd)
	if instance == "" {
		return blogTarget{}, errors.New("blog: no instance URL — pass --url, set YASAKU_URL, or configure http.baseURL")
	}
	prof, err := loadProfile(cfg.Session.Path, instance)
	if err != nil {
		return blogTarget{}, err
	}
	explicit, _, err := readToken(cmd)
	if err != nil {
		return blogTarget{}, err
	}
	key, err := credentialFor(instance, prof, explicit)
	if err != nil {
		return blogTarget{}, err
	}
	if key == "" {
		return blogTarget{}, apperror.New(
			apperror.CodeUnauthenticated,
			"blog: no API key for "+instance+" — pass --token, set YASAKU_TOKEN, or run `yasaku auth login`",
			codes.Unauthenticated,
		)
	}
	org := blogSlug(cmd, "org", "YASAKU_ORG", prof.Org)
	project := blogSlug(cmd, "project", "YASAKU_PROJECT", prof.Project)
	if org == "" || project == "" {
		return blogTarget{}, errors.New("blog: no active org or project — pass --org and --project")
	}
	return blogTarget{client: dataplane.NewClient(instance, key), org: org, project: project}, nil
}

func blogSlug(cmd *cobra.Command, flag, env, fallback string) string {
	if f := cmd.Root().PersistentFlags().Lookup(flag); f != nil && f.Changed {
		return strings.TrimSpace(f.Value.String())
	}
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v
	}
	return fallback
}

func renderPosts(cmd *cobra.Command, posts []dataplane.Post) error {
	out := cmd.OutOrStdout()
	switch render.Detect(cmd) {
	case render.FormatJSON:
		return render.JSON(out, blogPostMaps(posts))
	case render.FormatNDJSON:
		return render.NDJSON(out, func(yield func(any) bool) {
			for _, m := range blogPostMaps(posts) {
				if !yield(m) {
					return
				}
			}
		})
	default:
		rows := make([][]string, 0, len(posts))
		for _, p := range posts {
			rows = append(rows, []string{p.Slug, p.Status, fmt.Sprint(p.Version), p.Title})
		}
		return render.Table(out, []string{"SLUG", "STATUS", "VERSION", "TITLE"}, rows)
	}
}

func renderPost(cmd *cobra.Command, post dataplane.Post) error {
	out := cmd.OutOrStdout()
	switch render.Detect(cmd) {
	case render.FormatJSON:
		return render.JSON(out, blogPostMap(post))
	case render.FormatNDJSON:
		return render.NDJSON(out, func(yield func(any) bool) { yield(blogPostMap(post)) })
	default:
		_, err := fmt.Fprintf(out,
			"id:       %s\nslug:     %s\ntitle:    %s\nstatus:   %s\nversion:  %d\ncategory: %s\n",
			post.ID, post.Slug, post.Title, post.Status, post.Version, post.CategoryID)
		return err
	}
}

func blogPostMaps(posts []dataplane.Post) []map[string]any {
	out := make([]map[string]any, 0, len(posts))
	for _, p := range posts {
		out = append(out, blogPostMap(p))
	}
	return out
}

func blogPostMap(p dataplane.Post) map[string]any {
	return map[string]any{
		"id":         p.ID,
		"slug":       p.Slug,
		"title":      p.Title,
		"body":       p.Body,
		"categoryId": p.CategoryID,
		"status":     p.Status,
		"version":    p.Version,
	}
}

//nolint:cyclop // a flat mapping table; splitting it hides the contract it states.
func blogError(op string, err error) error {
	switch {
	case dataplane.IsUnauthorizedError(err):
		return blogAppError(apperror.CodeUnauthenticated, op+": the API key was rejected", codes.Unauthenticated, err)
	case dataplane.IsNotFoundError(err):
		return blogAppError(apperror.CodeNotFound, op+": no such org, project or post — or the key cannot see it", codes.NotFound, err)
	case dataplane.IsConflictError(err):
		return blogAppError(apperror.CodeAlreadyExists, op+": slug already taken, or the idempotency key was replayed with a different body", codes.AlreadyExists, err)
	case dataplane.IsPreconditionFailedError(err):
		return blogAppError(apperror.CodeValidation, op+": the post moved on since --if-version — re-read it with `yasaku blog get` and retry", codes.FailedPrecondition, err)
	case dataplane.IsPreconditionRequiredError(err):
		return blogAppError(apperror.CodeValidation, op+": this write needs --if-version", codes.FailedPrecondition, err)
	case dataplane.IsBadRequestError(err):
		return blogAppError(apperror.CodeValidation, op+": the data plane rejected the request as malformed", codes.InvalidArgument, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func blogAppError(code, message string, grpcCode codes.Code, cause error) error {
	return apperror.New(code, message, grpcCode).WithCause(cause)
}
