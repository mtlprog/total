# Total

Go web application with PostgreSQL database.

## Commands

- `make dev` - Start dev environment (builds Linux binary + docker compose up)
- `make dev-restart` - Restart after code changes
- `make dev-logs` - View container logs
- `make dev-down` - Stop dev environment
- `make db` - Start only PostgreSQL
- `make db-reset` - Reset database (removes all data)
- `make run` - Run locally (requires PostgreSQL running)
- `make test` - Run tests
- `make lint` - Format and vet code
- `make mocks` - Regenerate mocks with mockery

## Project Structure

```
cmd/total/         - CLI entry point
internal/
├── config/        - Configuration constants
├── database/      - PostgreSQL + goose migrations
├── handler/       - HTTP request handlers
├── logger/        - Structured logging (slog/JSON)
├── model/         - Data structures
├── repository/    - Data access layer (squirrel query builder)
├── service/       - Business logic
└── template/      - HTML templates
```

## Stack

- Go 1.24+
- PostgreSQL 16+
- goose migrations
- squirrel query builder
- pgx/v5 driver
- urfave/cli for CLI
- slog/JSON structured logging

## Development

1. Start database: `make db`
2. Run server: `make run`
3. Or use Docker: `make dev`
