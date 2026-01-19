package main

import (
	"context"
	"fmt"

	api "golang-test-task/api"
	"golang-test-task/sqlc"
)

type Server struct {
	queries *sqlc.Queries
}

func NewServer(queries *sqlc.Queries) *Server {
	return &Server{
		queries: queries,
	}
}

func (s *Server) AddNumber(ctx context.Context, request api.AddNumberRequestObject) (api.AddNumberResponseObject, error) {
	_, err := s.queries.InsertNumber(ctx, int32(request.Params.Number))
	if err != nil {
		return api.AddNumber500JSONResponse{
			Error: fmt.Sprintf("failed to insert number: %v", err),
		}, nil
	}

	numbers, err := s.queries.GetAllNumbersSorted(ctx)
	if err != nil {
		return api.AddNumber500JSONResponse{
			Error: fmt.Sprintf("failed to get numbers: %v", err),
		}, nil
	}

	result := make([]int, len(numbers))
	for i, num := range numbers {
		result[i] = int(num.Number)
	}

	return api.AddNumber200JSONResponse{
		Numbers: &result,
	}, nil
}
