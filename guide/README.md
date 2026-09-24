# From NestJS to tyr

The service [tasks](../tasks) written twice: [on NestJS and Knex](nestjs), as a team that knows them would write it, and on [tyr](https://github.com/tyr-go/tyr), the recipe. The same operations, the same errors, the same database. This guide goes through them side by side, from what you know to what it becomes in Go.

The snippets are the code of both services, which CI builds: [guide_test.go](guide_test.go) checks that each one is still the code it quotes.

## The map

| NestJS and Knex | tyr |
|---|---|
| A module, `@Module` | A package; `main` wires them, without a container |
| A controller: `@Post()`, `@Param()`, `@Body()` | The contract: `tyr.Define`, with `rest.Route` |
| A DTO with class-validator | A request type with `validate` tags, and a `Validate` method for the rest |
| A service, `@Injectable()` | A struct whose methods are the handlers: plain functions of a request |
| The query builder of Knex | SQL, from which sqlc generates Go |
| `knex.transaction(...)` | `db.InTx(ctx, ...)` |
| `throw new NotFoundException(...)` | `return nil, tyr.NotFound(...)`: a kind rather than a status |
| An exception filter | `api.MapError` |
| A guard, `@Roles()` and `Reflector` | Middleware that finds the caller, an interceptor that decides, and an option of the operation, `auth.Require` |
| `@CurrentCaller()` of `request.user` | `auth.CallerFrom(ctx)` |
| `@nestjs/swagger` and its decorators | Documents made of the types and the contract, OpenAPI and OpenRPC |
| `@nestjs/terminus` | The package `health` |
| The migrations of Knex | The migrations of goose, in SQL |
| `app.enableShutdownHooks()` | A drain, then `http.Server.Shutdown` |
| Jest and supertest | `go test`, with a real database per test |

## A route and its request

In NestJS, a DTO says what a request may be, with the decorators of class-validator, and the controller binds it to a route:

<!-- guide:nest:create-project-dto -->
```ts
export class CreateProjectDto {
  @ApiProperty({ description: 'A short code of the project, of A-Z and 0-9, starting with a letter, such as WEB.' })
  @IsString()
  @Length(2, 10)
  @Matches(/^[A-Z][A-Z0-9]*$/, { message: 'key must be of A-Z and 0-9, starting with a letter' })
  key: string;

  @ApiProperty({ description: 'The name of the project.' })
  @IsString()
  @Length(1, 100)
  name: string;
}
```

<!-- guide:nest:create-project-route -->
```ts
@Post()
@ApiOperation({ summary: 'Create a project' })
@ApiCreatedResponse({ type: ProjectDto })
@ApiConflictResponse({ description: 'The caller has a project of the key.' })
async create(
  @CurrentCaller() caller: Caller,
  @Body() dto: CreateProjectDto,
  @Res({ passthrough: true }) res: Response,
): Promise<ProjectDto> {
  const project = await this.projects.create(caller.id, dto);
  res.location(`/projects/${project.id}`);
  return project;
}
```

In tyr, the contract says it all in one value, `tyr.Define`: the name of the operation, its route, its status, its documentation and the errors it declares. The server implements it and its clients call it, so the compiler checks both sides:

<!-- guide:go:define-create-project -->
```go
CreateProject = tyr.Define[CreateProjectReq, ProjectCreated]("projects.create",
	rest.Route("POST /projects"), rest.Status(http.StatusCreated),
	tyr.Summary("Create a project"),
	tyr.Tags("projects"),
	tyr.Errors(tyr.KindAlreadyExists),
).Example("website",
	CreateProjectReq{Key: "WEB", Name: "Website"},
	ProjectCreated{Project: website, Location: "/projects/" + website.ID.String()},
)
```

The request is a type with tags: `json` names its member, `validate` states its rules, in the syntax of go-playground/validator, and `doc` describes it in the documents. What tags can't say goes in a `Validate` method, as a custom validator of class-validator would:

<!-- guide:go:create-project-req -->
```go
// CreateProjectReq is a request to create a project.
type CreateProjectReq struct {
	Key  string `json:"key" validate:"required,min=2,max=10" doc:"A short code of the project, of A-Z and 0-9, starting with a letter, such as WEB. The projects of a caller have keys of their own."`
	Name string `json:"name" validate:"required,max=100" doc:"The name of the project."`
}

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// Validate holds the rule tags can't express: the characters of the key.
func (r CreateProjectReq) Validate() error {
	var v tyr.Violations
	if !keyRe.MatchString(r.Key) {
		v.Add("key", "must be of A-Z and 0-9, starting with a letter")
	}
	return v.Err()
}
```

The caller isn't a parameter of the route: it's in the context of the call, where the middleware put it. The `Location` of the response is a field of the result, `ProjectCreated`, with `header:"Location"`, rather than a call on the response.

## The service

A service of NestJS gets its Knex by injection and throws exceptions of HTTP:

<!-- guide:nest:create-project -->
```ts
async create(owner: string, dto: CreateProjectDto): Promise<ProjectDto> {
  try {
    const [row] = await this.knex<ProjectRow>('projects')
      .insert({ id: uuidv7(), owner_id: owner, key: dto.key, name: dto.name })
      .returning('*');
    return toProject(row);
  } catch (error) {
    if (error instanceof DatabaseError && error.code === '23505') {
      throw new ConflictException(`a project with the key ${dto.key} exists`);
    }
    throw error;
  }
}
```

<!-- guide:nest:get-project -->
```ts
async get(owner: string, id: string): Promise<ProjectDto> {
  const row = await this.knex<ProjectRow>('projects').where({ id, owner_id: owner }).first();
  if (!row) {
    throw new NotFoundException(`project ${id} not found`);
  }
  return toProject(row);
}
```

A handler of tyr is a plain function of a context and a request, which returns a result or an error. It returns its errors rather than throwing them, and their kinds say what went wrong, whatever the transport: REST sends `already_exists` as 409, JSON-RPC with the code 409 and the kind in its data.

<!-- guide:go:create-project -->
```go
// CreateProject implements contract.CreateProject.
func (s *Service) CreateProject(ctx context.Context, req contract.CreateProjectReq) (contract.ProjectCreated, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.ProjectCreated{}, err
	}
	p, err := s.db.CreateProject(ctx, store.CreateProjectParams{ID: uuid.NewV7(), OwnerID: owner, Key: req.Key, Name: req.Name})
	if pgerr.Code(err) == pgerrcode.UniqueViolation {
		return contract.ProjectCreated{}, tyr.AlreadyExists("a project with the key %s exists", req.Key).WithCause(err)
	}
	if err != nil {
		return contract.ProjectCreated{}, err
	}
	return contract.ProjectCreated{Project: projectOf(p), Location: "/projects/" + p.ID.String()}, nil
}
```

<!-- guide:go:get-project -->
```go
// GetProject implements contract.GetProject.
func (s *Service) GetProject(ctx context.Context, req contract.ProjectReq) (contract.Project, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Project{}, err
	}
	p, err := s.db.GetProject(ctx, store.GetProjectParams{ID: req.ID, OwnerID: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.Project{}, projectNotFound(req.ID)
	}
	if err != nil {
		return contract.Project{}, err
	}
	return projectOf(p), nil
}
```

`errors.Is(err, pgx.ErrNoRows)` is the `if (!row)` of `.first()`, and `pgerr.Code(err)` the `error.code` of a `DatabaseError`.

## Queries

The query builder of Knex makes the SQL at run time, with its filters in `modify`:

<!-- guide:nest:list-tasks -->
```ts
async list(owner: string, projectId: string, query: ListTasksQuery): Promise<Page<TaskDto>> {
  // The project of another owner is not found, rather than empty.
  await this.projects.get(owner, projectId);
  const after = decodeCursor(query.cursor);
  const limit = query.limit ?? 20;
  const rows = await this.knex<TaskRow>('tasks')
    .where({ project_id: projectId })
    .modify((q) => {
      if (query.status) {
        q.where({ status: query.status });
      }
      if (after) {
        q.where('id', '<', after);
      }
    })
    .orderBy('id', 'desc')
    .limit(limit + 1); // one more than the page, to know if there's a next one
  const page: Page<TaskDto> = { items: rows.slice(0, limit).map(toTask) };
  if (rows.length > limit) {
    page.next_cursor = encodeCursor(rows[limit - 1].id);
  }
  return page;
}
```

With sqlc, the query is SQL, in a file, and sqlc generates the Go code that runs it, with types for its parameters and rows: a column that isn't there or a parameter of the wrong type fails at build time, not at run time. A filter that may be left out is a parameter that may be null:

<!-- guide:sql:ListTasks -->
```sql
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
```

The handler calls it as a method, and makes the page the same way:

<!-- guide:go:list-tasks -->
```go
// ListTasks implements contract.ListTasks.
func (s *Service) ListTasks(ctx context.Context, req contract.ListTasksReq) (contract.Page[contract.Task], error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	after, err := decodeCursor(req.Cursor)
	if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	// The project of another owner is not found, rather than empty.
	if _, err := s.db.GetProject(ctx, store.GetProjectParams{ID: req.ProjectID, OwnerID: owner}); errors.Is(err, pgx.ErrNoRows) {
		return contract.Page[contract.Task]{}, projectNotFound(req.ProjectID)
	} else if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	var status *string
	if req.Status != "" {
		status = &req.Status
	}
	limit := cmp.Or(req.Limit, defaultLimit)
	rows, err := s.db.ListTasks(ctx, store.ListTasksParams{ProjectID: req.ProjectID, Status: status, After: after, Max: int32(limit + 1)})
	if err != nil {
		return contract.Page[contract.Task]{}, err
	}
	page := contract.Page[contract.Task]{Items: []contract.Task{}}
	for i, t := range rows {
		if i == limit {
			page.NextCursor = encodeCursor(rows[i-1].ID)
			break
		}
		page.Items = append(page.Items, taskOf(t))
	}
	return page, nil
}
```

## Transactions

`knex.transaction` runs a function in a transaction, commits it if the function returns, and rolls it back if it throws:

<!-- guide:nest:create-task -->
```ts
async create(owner: string, projectId: string, dto: CreateTaskDto): Promise<TaskDto> {
  return this.knex.transaction(async (trx) => {
    // Numbering the task locks the project until the transaction ends,
    // and finds no project unless it's the caller's.
    const [project] = await trx<ProjectRow>('projects')
      .where({ id: projectId, owner_id: owner })
      .increment('last_number', 1)
      .returning('last_number');
    if (!project) {
      throw new NotFoundException(`project ${projectId} not found`);
    }
    const [row] = await trx<TaskRow>('tasks')
      .insert({
        id: uuidv7(),
        project_id: projectId,
        number: project.last_number,
        title: dto.title,
        status: dto.status ?? 'todo',
        due_at: dto.due_at ?? null,
      })
      .returning('*');
    return toTask(row);
  });
}
```

`db.InTx` does the same with a function that returns an error: nil commits, an error rolls back and comes back as it is. The queries of the transaction are those of `q`, as `trx` in Knex:

<!-- guide:go:create-task -->
```go
// CreateTask implements contract.CreateTask.
func (s *Service) CreateTask(ctx context.Context, req contract.CreateTaskReq) (contract.TaskCreated, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return contract.TaskCreated{}, err
	}
	var task store.Task
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		// Numbering the task locks the project until the transaction
		// ends, and finds no project unless it's the caller's.
		n, err := q.NextTaskNumber(ctx, store.NextTaskNumberParams{ID: req.ProjectID, OwnerID: owner})
		if errors.Is(err, pgx.ErrNoRows) {
			return projectNotFound(req.ProjectID)
		}
		if err != nil {
			return err
		}
		task, err = q.CreateTask(ctx, store.CreateTaskParams{
			ID:        uuid.NewV7(),
			ProjectID: req.ProjectID,
			Number:    n,
			Title:     req.Title,
			Status:    cmp.Or(req.Status, "todo"),
			DueAt:     req.DueAt,
		})
		return err
	})
	if err != nil {
		return contract.TaskCreated{}, err
	}
	return contract.TaskCreated{Task: taskOf(task), Location: "/tasks/" + task.ID.String()}, nil
}
```

## Errors of the database

An exception filter of NestJS turns the errors of PostgreSQL that the services let through into statuses of HTTP:

<!-- guide:nest:pg-error-filter -->
```ts
// PgErrorFilter answers the errors of PostgreSQL that the services leave
// as they are with the statuses they mean for the client.
@Catch(DatabaseError)
export class PgErrorFilter extends BaseExceptionFilter {
  catch(error: DatabaseError, host: ArgumentsHost): void {
    super.catch(this.toHttp(error) ?? error, host);
  }

  private toHttp(error: DatabaseError): HttpException | undefined {
    switch (error.code) {
      case '23505': // unique_violation
        return new ConflictException('already exists');
      case '23503': // foreign_key_violation
        return new ConflictException('conflicts with related records');
      case '23514': // check_violation
        return new BadRequestException('a value is invalid');
      case '57014': // query_canceled, as by statement_timeout
        return new GatewayTimeoutException('the query took too long');
      case '40001': // serialization_failure
      case '40P01': // deadlock_detected
        return new ServiceUnavailableException('the query conflicted with another: try again');
    }
    return undefined; // a 500
  }
}
```

`api.MapError` gets the errors of the handlers that aren't errors of tyr already, and translates them into kinds, for every transport. `pgerr.Map` is such a function:

<!-- guide:go:pgerr-map -->
```go
func Map(err error) error {
	switch Code(err) {
	case pgerrcode.UniqueViolation:
		return tyr.AlreadyExists("already exists").WithCause(err)
	case pgerrcode.ForeignKeyViolation:
		return tyr.FailedPrecondition("conflicts with related records").WithCause(err)
	case pgerrcode.CheckViolation:
		return tyr.InvalidArgument("a value is invalid").WithCause(err)
	case pgerrcode.QueryCanceled:
		return tyr.DeadlineExceeded("the query took too long").WithCause(err)
	case pgerrcode.SerializationFailure, pgerrcode.DeadlockDetected:
		return tyr.Unavailable("the query conflicted with another: try again").WithCause(err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return tyr.NotFound("not found").WithCause(err)
	}
	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return tyr.Unavailable("the database is unavailable").WithCause(err)
	}
	return nil
}
```

An error that no mapper translates is internal: the client gets `internal error` without its details, which go to the log with the request ID, as the filter of NestJS answers 500.

## Guards: who calls, and what they may

NestJS has guards: one that verifies the token and puts the caller into the request, and one that reads the roles that a decorator gave the route:

<!-- guide:nest:auth-guard -->
```ts
// AuthGuard lets through the requests with a valid bearer token, with
// their caller in request.user, and those of Public routes.
@Injectable()
export class AuthGuard implements CanActivate {
  constructor(
    private readonly jwt: JwtService,
    private readonly reflector: Reflector,
  ) {}

  async canActivate(context: ExecutionContext): Promise<boolean> {
    if (this.reflector.getAllAndOverride<boolean>(IS_PUBLIC, [context.getHandler(), context.getClass()])) {
      return true;
    }
    const request = context.switchToHttp().getRequest<Request & { user?: Caller }>();
    const [type, token] = request.headers.authorization?.split(' ') ?? [];
    if (type !== 'Bearer' || !token) {
      throw new UnauthorizedException('a bearer token is required');
    }
    try {
      const payload = await this.jwt.verifyAsync<{ sub: string; roles?: string[] }>(token);
      request.user = { id: payload.sub, roles: payload.roles ?? [] };
    } catch {
      throw new UnauthorizedException('the bearer token is invalid');
    }
    return true;
  }
}
```

<!-- guide:nest:roles-guard -->
```ts
// RolesGuard lets through the requests of the routes that Roles marks only
// if their caller has one of the roles.
@Injectable()
export class RolesGuard implements CanActivate {
  constructor(private readonly reflector: Reflector) {}

  canActivate(context: ExecutionContext): boolean {
    const roles = this.reflector.get(Roles, context.getHandler());
    if (!roles) {
      return true;
    }
    const { user } = context.switchToHttp().getRequest<{ user: Caller }>();
    if (!roles.some((role) => user.roles.includes(role))) {
      throw new ForbiddenException(`requires the role ${roles.join(' or ')}`);
    }
    return true;
  }
}
```

<!-- guide:nest:stats-route -->
```ts
@Get('stats')
@Roles(['admin'])
@ApiOperation({ summary: 'Count the projects and tasks of all owners' })
@ApiOkResponse({ type: StatsDto })
@ApiForbiddenResponse({ description: 'The caller lacks the role admin.' })
stats(): Promise<StatsDto> {
  return this.admin.stats();
}
```

tyr splits the first guard in two. HTTP middleware, `auth.Authenticate`, only finds out who calls, and puts the caller into the context; it never answers itself. An interceptor, which runs around every call of every operation, over REST and JSON-RPC alike, decides by the option that the operation, or its group, has, as the decorator `@Roles` marks a route:

<!-- guide:go:auth-interceptor -->
```go
// Interceptor rejects the calls of the operations marked by Require that
// lack a caller, with unauthenticated, or whose caller lacks the roles,
// with permission_denied. It passes on the calls of other operations.
func Interceptor(ctx context.Context, op *tyr.Operation, req any, next tyr.Invoker) (any, error) {
	roles, ok := rolesKey.Get(op)
	if !ok {
		return next(ctx, req)
	}
	c, ok := CallerFrom(ctx)
	if !ok {
		problem, ok := problemKey.Get(ctx)
		if !ok {
			problem = "a bearer token is required"
		}
		return nil, tyr.Unauthenticated("%s", problem)
	}
	if len(roles) > 0 && !slices.ContainsFunc(roles, c.HasRole) {
		return nil, tyr.PermissionDenied("%s requires the role %s", op.Name(), strings.Join(roles, " or "))
	}
	return next(ctx, req)
}
```

## Modules and bootstrap

NestJS assembles the application from modules, and `main.ts` configures it:

<!-- guide:nest:app-module -->
```ts
@Module({
  imports: [DatabaseModule, AuthModule, ProjectsModule, TasksModule, AdminModule, HealthModule],
})
export class AppModule {}
```

<!-- guide:nest:bootstrap -->
```ts
async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  app.useGlobalPipes(new ValidationPipe({ whitelist: true, transform: true }));
  const { httpAdapter } = app.get(HttpAdapterHost);
  app.useGlobalFilters(new PgErrorFilter(httpAdapter));
  app.enableCors({ origin: process.env.ORIGINS?.split(','), exposedHeaders: ['Location'] });
  app.enableShutdownHooks();

  const config = new DocumentBuilder().setTitle('tasks').setVersion('1.0.0').addBearerAuth().build();
  SwaggerModule.setup('docs', app, () => SwaggerModule.createDocument(app, config));

  await app.listen(process.env.PORT ?? 8080);
}
```

A program of tyr has no container: `main` makes the values and passes them on. `newAPI` registers the operations, with the interceptors and the translation of errors; groups give options to the operations they have, as a decorator of a class does to its routes:

<!-- guide:go:new-api -->
```go
// newAPI returns the API of the service: its operations, the interceptors
// that trace them and authorize their callers, and the translation of the
// errors of the database.
func newAPI(svc *service.Service, logger *slog.Logger) *tyr.API {
	api := tyr.New(tyr.WithLogger(logger))
	// First, so that its spans cover the others.
	api.Use(oteltyr.Interceptor(), auth.Interceptor)
	api.MapError(pgerr.Map)

	// The contract has the names and the routes; who may call what, and
	// for how long, is the server's business. Every operation needs a
	// caller and has 5 seconds, within the WriteTimeout of the server, and
	// may find the database unavailable.
	ops := api.Group(auth.Require(), tyr.Timeout(5*time.Second), tyr.Errors(tyr.KindUnavailable))
	ops.Implement(contract.CreateProject, svc.CreateProject)
	ops.Implement(contract.GetProject, svc.GetProject)
	ops.Implement(contract.ListProjects, svc.ListProjects)
	ops.Implement(contract.DeleteProject, svc.DeleteProject)
	ops.Implement(contract.CreateTask, svc.CreateTask)
	ops.Implement(contract.GetTask, svc.GetTask)
	ops.Implement(contract.ListTasks, svc.ListTasks)
	ops.Implement(contract.UpdateTask, svc.UpdateTask)
	ops.Implement(contract.DeleteTask, svc.DeleteTask)

	admin := ops.Group(auth.Require("admin"))
	admin.Implement(contract.GetStats, svc.GetStats)
	return api
}
```

`newServer` is the rest of `main.ts`: the routes of REST and the endpoint of JSON-RPC, their documents, CORS, and the middleware, as `app.use` has it. The pipe of validation needs no line: tyr checks every request. The probes of health go past the middleware, on an outer mux:

<!-- guide:go:new-server -->
```go
// newServer returns the HTTP server of the service: the operations of api
// and their documents, the middleware around them, which authenticates
// tokens signed with key, and the probes past it, with the timeouts of a
// server that faces the internet. The pages of origins may call the
// service from browsers.
func newServer(addr string, api *tyr.API, key []byte, origins []string, ready *health.Readiness, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	routes := rest.Mount(mux, api, mountOptions...)
	mux.Handle("GET /openapi.json", routes.OpenAPI(info))
	mux.Handle("POST /rpc", jsonrpc.Handler(api, jsonrpc.Discover(info)))

	cors := middleware.CORS{
		Origins: origins,
		// The resource that a create makes, and the ID of a request to
		// report.
		Expose: []string{"Location", "X-Request-ID"},
		MaxAge: time.Hour,
	}
	// No protection against cross-site requests: the service reads its
	// callers from the header Authorization, which a browser doesn't add
	// to a request by itself, as it does cookies, so a page of another
	// site can't call it on its user's behalf.

	// The probes go past the middleware: the balancers call them every few
	// seconds, and the access log would drown in their records.
	root := http.NewServeMux()
	root.Handle("GET /health/live", health.Live())
	root.Handle("GET /health/ready", ready)
	root.Handle("/", oteltyr.Handler(middleware.Chain(rest.ProblemHandler(mux), // first = outermost
		middleware.RequestID(),
		middleware.Logger(logger),
		// It answers preflight requests before the mux, which would
		// answer them with 405, and Recover keeps its headers on a 500.
		cors.Handler,
		middleware.Recover(logger),
		// It only finds out who calls: the interceptor of auth answers
		// the calls that need a caller and have none.
		auth.Authenticate(key),
	)))

	return &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}
```

Where `enableShutdownHooks` closes the application, `serve` in `main.go` drains first: readiness fails, and the server serves on for a few seconds while the balancers take the traffic away, before it shuts down.

## Migrations

A migration of Knex builds the schema with its builder:

<!-- guide:nest:migration -->
```ts
export async function up(knex: Knex): Promise<void> {
  await knex.schema.createTable('projects', (t) => {
    t.uuid('id').primary(); // a UUIDv7, made by the service
    t.text('owner_id').notNullable();
    t.text('key').notNullable().checkRegex('^[A-Z][A-Z0-9]{1,9}$');
    t.text('name').notNullable().checkLength('<=', 100);
    t.integer('last_number').notNullable().defaultTo(0); // of the last task created
    t.timestamp('created_at', { useTz: true }).notNullable().defaultTo(knex.fn.now());
    t.unique(['owner_id', 'key']);
    t.index(['owner_id', 'id']);
  });

  // A project with tasks can't be deleted: the foreign key has no cascade.
  await knex.schema.createTable('tasks', (t) => {
    t.uuid('id').primary();
    t.uuid('project_id').notNullable().references('id').inTable('projects');
    t.integer('number').notNullable();
    t.text('title').notNullable().checkLength('<=', 200);
    t.text('status').notNullable().defaultTo('todo').checkIn(['todo', 'doing', 'done']);
    t.timestamp('due_at', { useTz: true });
    t.timestamp('created_at', { useTz: true }).notNullable().defaultTo(knex.fn.now());
    t.timestamp('updated_at', { useTz: true }).notNullable().defaultTo(knex.fn.now());
    t.unique(['project_id', 'number']);
    t.index(['project_id', 'id']);
  });
}

export async function down(knex: Knex): Promise<void> {
  await knex.schema.dropTable('tasks');
  await knex.schema.dropTable('projects');
}
```

A migration of goose is SQL. The migrations are in the binary, and sqlc reads them as the schema of the queries:

<!-- guide:file:tasks/store/migrations/00001_projects_and_tasks.sql -->
```sql
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
```

## Health

Terminus runs indicators, such as a ping of Knex:

<!-- guide:nest:health -->
```ts
// KnexHealthIndicator checks that the database answers.
@Injectable()
export class KnexHealthIndicator {
  constructor(
    @InjectKnex() private readonly knex: Knex,
    private readonly health: HealthIndicatorService,
  ) {}

  async ping(key: string) {
    const indicator = this.health.check(key);
    try {
      await this.knex.raw('select 1');
      return indicator.up();
    } catch {
      return indicator.down();
    }
  }
}

@Public()
@Controller('health')
export class HealthController {
  constructor(
    private readonly health: HealthCheckService,
    private readonly db: KnexHealthIndicator,
  ) {}

  @Get('live')
  live() {
    return { status: 'ok' };
  }

  @Get('ready')
  @HealthCheck()
  ready() {
    return this.health.check([() => this.db.ping('db')]);
  }
}
```

The package `health` of tyr is `health.Live()` and `health.NewReadiness(health.Check("db", db.Ping))`, on the outer mux of `newServer`: the checks run in parallel, each within a second, and the responses tell statuses only, while the errors go to the log.

## Documents

`@nestjs/swagger` builds the document from decorators: `@ApiProperty` on every field of every DTO, and a decorator per response, which say again what the types and the code say already, and drift apart from them.

tyr builds the documents from what the server runs on: the types of the requests and results, their `validate` and `doc` tags, and the options of the contract, such as `tyr.Summary` and `tyr.Errors`. It makes an OpenAPI document for REST and an OpenRPC one for JSON-RPC, and types the errors by status: a client generated from the document knows that a 409 of `projects.create` is `already_exists`. The service keeps them in [tasks/api](../tasks/api), and [tasks/clients/ts](../tasks/clients/ts) is a client in TypeScript made from them.

## Tests

Where Jest runs the application with supertest against a database, `go test` runs the handler of the service with `httptest`, over REST and over JSON-RPC with the typed client of the contract, each test on a database of its own, a copy of a template that has the migrations: [tasks/main_test.go](../tasks/main_test.go). Time is fake where it matters: the drain of 5 seconds takes none, with `testing/synctest`.

## What changes

- **Errors are values.** A handler returns them, with a kind; nothing is thrown, and the kinds are the same over every transport.
- **No decorators.** What NestJS keeps in the metadata of classes is in values: the contract, the options of operations, the tags of types.
- **No container.** `main` makes everything and passes it on, so that the code tells what depends on what.
- **One contract, two transports.** The operations are served over REST and JSON-RPC, and called from Go by a typed client, without generated code.
- **The caller is in the context.** Handlers take the context of the call, and the middleware and the interceptors put what they know into it.

## Run both

The twins share their tokens: a token of `cmd/token` of tasks, signed with the same `JWT_SECRET`, works for both. Each needs a database of its own, as their migrations differ in form. In two terminals, from the root of the repository, with the same `JWT_SECRET`:

```sh
export JWT_SECRET=$(openssl rand -hex 32)
psql postgres://postgres:postgres@localhost:5432/postgres -c 'CREATE DATABASE tasks' -c 'CREATE DATABASE nest'

# tyr, at :8080
cd tasks
DATABASE_URL=postgres://postgres:postgres@localhost:5432/tasks go run . -migrate

# NestJS, at :8081
cd guide/nestjs && npm ci && npm run build
export DATABASE_URL=postgres://postgres:postgres@localhost:5432/nest
npm run migrate && PORT=8081 npm start
```

Then call both with one token:

```sh
TOKEN=$(cd tasks && go run ./cmd/token -sub alice)
for port in 8080 8081; do
	curl -i localhost:$port/projects -H "Authorization: Bearer $TOKEN" \
		-H 'Content-Type: application/json' -d '{"key":"WEB","name":"Website"}'
done
```

The answers match but for the form of errors: NestJS writes its own, `{"message":…,"error":…,"statusCode":…}`, and tyr problems of RFC 9457, with the kind of the error.
