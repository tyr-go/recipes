-- name: CreateTask :one
INSERT INTO tasks (id, project_id, number, title, status, due_at)
VALUES (@id, @project_id, @number, @title, @status, @due_at)
RETURNING *;

-- name: GetTask :one
-- A task of a project of the owner.
SELECT tasks.* FROM tasks
JOIN projects ON projects.id = tasks.project_id
WHERE tasks.id = @id AND projects.owner_id = @owner_id;

-- name: ListTasks :many
-- The tasks of a project, newest first, of the status if it isn't null,
-- after the task after if it isn't null. The project must be the owner's:
-- check it first.
SELECT * FROM tasks
WHERE project_id = @project_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('after')::uuid IS NULL OR id < sqlc.narg('after'))
ORDER BY id DESC
LIMIT @max;

-- name: UpdateTask :one
-- Sets the fields that aren't null, of a task of a project of the owner.
UPDATE tasks
SET title = coalesce(sqlc.narg('title'), tasks.title),
    status = coalesce(sqlc.narg('status'), tasks.status),
    due_at = coalesce(sqlc.narg('due_at'), tasks.due_at),
    updated_at = now()
FROM projects
WHERE tasks.id = @id
  AND projects.id = tasks.project_id
  AND projects.owner_id = @owner_id
RETURNING tasks.*;

-- name: DeleteTask :execrows
DELETE FROM tasks
USING projects
WHERE tasks.id = @id
  AND projects.id = tasks.project_id
  AND projects.owner_id = @owner_id;
