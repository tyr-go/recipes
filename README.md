# ᛏ tyr recipes

[![CI](https://github.com/tyr-go/recipes/actions/workflows/ci.yml/badge.svg)](https://github.com/tyr-go/recipes/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Services built with [tyr](https://github.com/tyr-go/tyr), the way you would build yours. Each recipe is a Go module of its own: copy its folder, and it is the start of a project.

| Recipe | What it shows |
|---|---|
| [tasks](tasks) | Projects and tasks on PostgreSQL: pgx and sqlc, migrations by goose, the errors of the database as kinds of tyr, tests on a real database |

## Use a recipe

A recipe needs Go 1.27 or later, as tyr does, and whatever its README lists, such as PostgreSQL. To start a project from one, copy its folder and give the module your path:

```sh
cp -r recipes/tasks myservice && cd myservice
go mod edit -module example.com/myservice
find . -name '*.go' -exec perl -pi -e 's|github.com/tyr-go/recipes/tasks|example.com/myservice|g' {} +
```

## License

[MIT](LICENSE)
