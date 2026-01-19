package tools

//go:generate go tool oapi-codegen -package api -generate std-http-server,strict-server -o ../api/server.go ../openapi.yaml
//go:generate go tool oapi-codegen -package api -generate client -o ../api/client.go ../openapi.yaml
//go:generate go tool oapi-codegen -package api -generate models -o ../api/models.go ../openapi.yaml

//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc generate -f ../sqlc.yaml
