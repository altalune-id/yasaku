-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.Schema}}.{{.TablePrefix}}blog_categories (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  slug                TEXT NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id, slug)
);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}blog_tags (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  slug                TEXT NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id, slug)
);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}blog_posts (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  -- NOTE: RESTRICT, not CASCADE: deleting a category that still has posts must fail loudly as CAT004.
  category_id         UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}blog_categories(id) ON DELETE RESTRICT,
  title               TEXT NOT NULL,
  slug                TEXT NOT NULL,
  body_markdown       TEXT NOT NULL DEFAULT '',
  status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
  first_published_at  TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id, slug),
  -- NOTE: target for the join table's composite FK, so a join row cannot name another org's post.
  UNIQUE (id, org_id)
);

CREATE INDEX {{.TablePrefix}}blog_posts_org_project_created_idx
  ON {{.Schema}}.{{.TablePrefix}}blog_posts (org_id, project_id, created_at DESC);

CREATE INDEX {{.TablePrefix}}blog_posts_category_idx
  ON {{.Schema}}.{{.TablePrefix}}blog_posts (category_id);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}blog_post_tags (
  post_id             UUID NOT NULL,
  tag_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}blog_tags(id) ON DELETE RESTRICT,
  -- NOTE: a join table needs its own RLS policy, which needs its own org_id.
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, tag_id),
  FOREIGN KEY (post_id, org_id)
    REFERENCES {{.Schema}}.{{.TablePrefix}}blog_posts(id, org_id) ON DELETE CASCADE
);

CREATE INDEX {{.TablePrefix}}blog_post_tags_tag_idx
  ON {{.Schema}}.{{.TablePrefix}}blog_post_tags (tag_id);

{{if .RLSEnforce}}
ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_categories FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}blog_categories_tenant
  ON {{.Schema}}.{{.TablePrefix}}blog_categories
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}blog_tags_tenant
  ON {{.Schema}}.{{.TablePrefix}}blog_tags
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_posts ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_posts FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}blog_posts_tenant
  ON {{.Schema}}.{{.TablePrefix}}blog_posts
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_post_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}blog_post_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}blog_post_tags_tenant
  ON {{.Schema}}.{{.TablePrefix}}blog_post_tags
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
{{end}}

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}blog_post_tags;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}blog_posts;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}blog_tags;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}blog_categories;

-- +goose StatementEnd
