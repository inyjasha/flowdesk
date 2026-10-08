CREATE TABLE users (
 id BIGSERIAL PRIMARY KEY,
 email TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 password_hash TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);
CREATE TABLE workspaces (
 id BIGSERIAL PRIMARY KEY,
 name TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE memberships (
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK (role IN ('owner','editor','viewer')),
 PRIMARY KEY(workspace_id,user_id)
);
CREATE INDEX memberships_user_idx ON memberships(user_id);
CREATE TABLE invites (
 token_hash TEXT PRIMARY KEY,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK (role IN ('editor','viewer')),
 expires_at TIMESTAMPTZ NOT NULL,
 used_at TIMESTAMPTZ
);
CREATE TABLE tasks (
 id BIGSERIAL PRIMARY KEY,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 title TEXT NOT NULL CHECK (length(title)>0),
 description TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK (status IN ('todo','in_progress','done')),
 priority TEXT NOT NULL CHECK (priority IN ('low','medium','high')),
 assignee_id BIGINT,
 due_date TIMESTAMPTZ,
 version INTEGER NOT NULL DEFAULT 1 CHECK (version>0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,assignee_id) REFERENCES memberships(workspace_id,user_id)
);
CREATE INDEX tasks_workspace_idx ON tasks(workspace_id,id DESC);
CREATE INDEX tasks_assignee_idx ON tasks(workspace_id,assignee_id);
CREATE TABLE activities (
 id BIGSERIAL PRIMARY KEY,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 task_id BIGINT NOT NULL,
 actor_id BIGINT NOT NULL REFERENCES users(id),
 action TEXT NOT NULL,
 title TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX activities_workspace_idx ON activities(workspace_id,id DESC);
CREATE TABLE outbox (
 id BIGSERIAL PRIMARY KEY,
 workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 payload JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
