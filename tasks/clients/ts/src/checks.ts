// Checks of the generated types, which tsc makes: the errors of an
// operation are typed by status, as the document declares them, so that a
// client can tell them apart. A document without those types fails them.
import type { paths } from "./schema";

/** Is true if A and B are the same type. */
type Equal<A, B> = (<T>() => T extends A ? 1 : 2) extends <T>() => T extends B ? 1 : 2 ? true : false;

type CreateProject = paths["/projects"]["post"]["responses"];
type DeleteProject = paths["/projects/{id}"]["delete"]["responses"];

/** A problem with the kind and the type of its status. */
type Of<R, Status extends keyof R> = R[Status] extends { content: { "application/problem+json": infer P } } ? P : never;

export const checks: [
  Equal<Of<CreateProject, 409>["kind"], "already_exists">,
  Equal<Of<CreateProject, 409>["type"], "/problems/already_exists">,
  Equal<Of<CreateProject, 400>["kind"], "invalid_argument">,
  Equal<Of<CreateProject, 401>["kind"], "unauthenticated">,
  Equal<Of<DeleteProject, 409>["kind"], "failed_precondition">,
  Equal<Of<DeleteProject, 404>["kind"], "not_found">,
] = [true, true, true, true, true, true];
