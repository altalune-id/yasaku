-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.TablePrefix}}blog_categories (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  slug                TEXT NOT NULL,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (project_id, slug)
);

CREATE TABLE {{.TablePrefix}}blog_tags (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  slug                TEXT NOT NULL,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (project_id, slug)
);

CREATE TABLE {{.TablePrefix}}blog_posts (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  -- NOTE: RESTRICT, not CASCADE: deleting a category that still has posts must fail loudly as CAT004.
  category_id         TEXT NOT NULL REFERENCES {{.TablePrefix}}blog_categories(id) ON DELETE RESTRICT,
  title               TEXT NOT NULL,
  slug                TEXT NOT NULL,
  body_markdown       TEXT NOT NULL DEFAULT '',
  status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
  first_published_at  TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (project_id, slug),
  -- NOTE: target for the join table's composite FK, so a join row cannot name another org's post.
  UNIQUE (id, org_id)
);

CREATE INDEX {{.TablePrefix}}blog_posts_org_project_created_idx
  ON {{.TablePrefix}}blog_posts (org_id, project_id, created_at DESC);

CREATE INDEX {{.TablePrefix}}blog_posts_category_idx
  ON {{.TablePrefix}}blog_posts (category_id);

CREATE TABLE {{.TablePrefix}}blog_post_tags (
  post_id             TEXT NOT NULL,
  tag_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}blog_tags(id) ON DELETE RESTRICT,
  -- NOTE: org_id mirrors the Postgres join table so both engines share one row shape.
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, tag_id),
  FOREIGN KEY (post_id, org_id)
    REFERENCES {{.TablePrefix}}blog_posts(id, org_id) ON DELETE CASCADE
);

CREATE INDEX {{.TablePrefix}}blog_post_tags_tag_idx
  ON {{.TablePrefix}}blog_post_tags (tag_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS {{.TablePrefix}}blog_post_tags;
DROP TABLE IF EXISTS {{.TablePrefix}}blog_posts;
DROP TABLE IF EXISTS {{.TablePrefix}}blog_tags;
DROP TABLE IF EXISTS {{.TablePrefix}}blog_categories;

-- +goose StatementEnd
