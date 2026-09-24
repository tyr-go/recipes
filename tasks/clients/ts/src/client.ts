// A typed client of the service tasks: openapi-fetch, with the types that
// openapi-typescript generates from the OpenAPI document of the service
// into schema.d.ts. After a change to the document, run npm run generate.
import createClient, { type Client } from "openapi-fetch";
import type { components, paths } from "./schema";

export type Project = components["schemas"]["Project"];
export type Task = components["schemas"]["Task"];
export type Problem = components["schemas"]["Problem"];
export type Status = components["schemas"]["Status"];

/** A client of the service at baseUrl, which calls it with a token. */
export function tasksClient(baseUrl: string, token: string): Client<paths> {
  const client = createClient<paths>({ baseUrl });
  client.use({
    onRequest({ request }) {
      request.headers.set("Authorization", `Bearer ${token}`);
      return request;
    },
  });
  return client;
}

/** A problem that the service answered with. */
export class TasksError extends Error {
  constructor(readonly problem: Problem) {
    super(problem.detail ?? problem.title);
  }
}

/**
 * Creates a project. The errors are typed by status: kind tells which of
 * those that the document declares for projects.create it is, and a 400
 * tells which fields are wrong.
 */
export async function createProject(client: Client<paths>, key: string, name: string): Promise<Project> {
  const { data, error } = await client.POST("/projects", { body: { key, name } });
  if (error) {
    switch (error.kind) {
      case "already_exists":
        throw new TasksError({ ...error, detail: `you have a project ${key} already` });
      case "invalid_argument":
        throw new TasksError({
          ...error,
          detail: (error.errors ?? []).map((v) => `${v.pointer}: ${v.detail}`).join("; ") || error.detail,
        });
      default:
        throw new TasksError(error);
    }
  }
  return data;
}

/** Lists the tasks of a project, of a status or of any, page after page. */
export async function* allTasks(client: Client<paths>, projectId: string, status?: Status): AsyncGenerator<Task> {
  let cursor: string | undefined;
  do {
    const { data, error } = await client.GET("/projects/{project_id}/tasks", {
      params: { path: { project_id: projectId }, query: { status, cursor } },
    });
    if (error) {
      throw new TasksError(error);
    }
    yield* data.items;
    cursor = data.next_cursor;
  } while (cursor);
}

/** Marks a task done, and returns it as it is now. */
export async function finish(client: Client<paths>, taskId: string): Promise<Task> {
  const { data, error } = await client.PATCH("/tasks/{id}", {
    params: { path: { id: taskId } },
    body: { status: "done" },
  });
  if (error) {
    throw new TasksError(error);
  }
  return data;
}
