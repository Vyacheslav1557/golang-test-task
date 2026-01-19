# Go Number Service

A Go microservice with REST API for working with numbers. Accepts a number, stores it in PostgreSQL, and returns a sorted list of all numbers.

## 🚀 Quick Start

### Requirements

- [Docker](https://www.docker.com/get-started) and Docker Compose
- [Go 1.24+](https://go.dev/dl/) (for local development)

### Running with Docker Compose


```bash
docker-compose up --build
```

The service will be available at: `http://localhost:8080`

## 🧪 Testing

### Running Integration Tests

The project uses [testcontainers-go](https://golang.testcontainers.org/) to automatically spin up PostgreSQL in a Docker container.

**Requirements:**
- Docker must be running
- Docker Desktop (for Windows/Mac) or Docker Engine (for Linux)

```bash
# Run all tests
go test ./tests/... -v

# With coverage output
go test ./tests/... -v -cover
```

## 🔧 Code Generation

The project uses code generation tools:

[sqlc](https://github.com/sqlc-dev/sqlc) for generating code from SQL files.
[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) for generating code from OpenAPI specification.
[goose](https://github.com/pressly/goose) for running migrations.

To generate code, use the following command:

```bash
go generate ./...
```
