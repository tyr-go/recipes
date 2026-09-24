-- name: CreateProject :one
INSERT INTO projects (id, owner_id, key, name)
VALUES (@id, @owner_id, @key, @name)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = @id AND owner_id = @owner_id;

-- name: ListProjects :many
-- The projects of an owner, newest first, after the project after if it
-- isn't null.
SELECT * FROM projects
WHERE owner_id = @owner_id
  AND (sqlc.narg('after')::uuid IS NULL OR id < sqlc.narg('after'))
ORDER BY id DESC
LIMIT @max;

-- name: DeleteProject :execrows
DELETE FROM projects
WHERE id = @id AND owner_id = @owner_id;

-- name: NextTaskNumber :one
-- Numbers a new task of a project. The update locks the row of the
-- project until the transaction ends, so that tasks created at once get
-- numbers one after another.
UPDATE projects
SET last_number = last_number + 1
WHERE id = @id AND owner_id = @owner_id
RETURNING last_number;

-- name: CountAll :one
-- The number of projects and tasks, and of the tasks done, of all owners.
SELECT
    (SELECT count(*) FROM projects) AS projects,
    (SELECT count(*) FROM tasks) AS tasks,
    (SELECT count(*) FROM tasks WHERE status = 'done') AS done;
