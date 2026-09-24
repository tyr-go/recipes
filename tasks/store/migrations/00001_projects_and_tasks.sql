-- +goose Up

-- A project of an owner, the subject of a token. Its key is a short code,
-- unique among the projects of the owner, such as WEB, and its tasks are
-- numbered within it: WEB-1, WEB-2, and so on.
CREATE TABLE projects (
    id          uuid PRIMARY KEY, -- a UUIDv7, made by the service
    owner_id    text NOT NULL,
    key         text NOT NULL,
    name        text NOT NULL,
    last_number integer NOT NULL DEFAULT 0, -- of the last task created
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_key_check CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    CONSTRAINT projects_name_check CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT projects_owner_id_key_key UNIQUE (owner_id, key)
);

-- The projects of an owner, newest first: UUIDv7s grow with time.
CREATE INDEX projects_owner_id_id_idx ON projects (owner_id, id DESC);

-- A task of a project, numbered within it. A project with tasks can't be
-- deleted: the foreign key has no ON DELETE CASCADE.
CREATE TABLE tasks (
    id         uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id),
    number     integer NOT NULL,
    title      text NOT NULL,
    status     text NOT NULL DEFAULT 'todo',
    due_at     timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tasks_title_check CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT tasks_status_check CHECK (status IN ('todo', 'doing', 'done')),
    CONSTRAINT tasks_project_id_number_key UNIQUE (project_id, number)
);

-- The tasks of a project, newest first, with or without a status.
CREATE INDEX tasks_project_id_id_idx ON tasks (project_id, id DESC);

-- +goose Down
DROP TABLE tasks;
DROP TABLE projects;
