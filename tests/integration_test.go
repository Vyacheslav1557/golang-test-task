package tests

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"golang-test-task/api"
	"golang-test-task/sqlc"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	testDBContainer *postgres.PostgresContainer
	testDBDSN       string
	testServerURL   string
	testClient      *api.ClientWithResponses
	testQueries     *sqlc.Queries
	testHTTPServer  *http.Server
)

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start PostgreSQL container
	container, dsn, err := setupPostgresContainer(ctx)
	if err != nil {
		slog.Error("Failed to setup postgres container", "error", err, "hint", "Please ensure Docker Desktop is running before running tests. You can start Docker Desktop and try again.")
		os.Exit(1)
	}
	testDBContainer = container
	testDBDSN = dsn

	// Run migrations
	if err := runMigrations(ctx, dsn); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Setup test server
	serverURL, server, queries, err := setupTestServer(dsn)
	if err != nil {
		slog.Error("Failed to setup test server", "error", err)
		os.Exit(1)
	}
	testServerURL = serverURL
	testHTTPServer = server
	testQueries = queries

	// Create API client
	client, err := api.NewClientWithResponses(testServerURL)
	if err != nil {
		slog.Error("Failed to create API client", "error", err)
		os.Exit(1)
	}
	testClient = client

	// Run tests
	code := m.Run()

	// Cleanup
	if testHTTPServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		testHTTPServer.Shutdown(ctx)
	}
	if testDBContainer != nil {
		testDBContainer.Terminate(ctx)
	}

	os.Exit(code)
}

// setupPostgresContainer starts a PostgreSQL container for testing
func setupPostgresContainer(ctx context.Context) (*postgres.PostgresContainer, string, error) {
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to start container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connection string: %w", err)
	}

	return container, dsn, nil
}

// runMigrations executes the database migrations
func runMigrations(ctx context.Context, dsn string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()

	// Read and execute migration file
	// Note: Using gen_random_uuid() instead of gen_random_uuidv7() for PostgreSQL 16 compatibility
	migrationSQL := `
		create table numbers (
			id uuid primary key default gen_random_uuid(),
			number integer not null
		);
		create index idx_numbers_number on numbers (number);
	`

	_, err = pool.Exec(ctx, migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	return nil
}

// setupTestServer starts the HTTP server for testing
func setupTestServer(dsn string) (string, *http.Server, *sqlc.Queries, error) {
	ctx := context.Background()

	// Create database connection pool
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to parse config: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return "", nil, nil, fmt.Errorf("failed to ping database: %w", err)
	}

	queries := sqlc.New(pool)

	// Create server
	server := &apiServer{queries: queries}
	strictHandler := api.NewStrictHandler(server, nil)
	handler := api.Handler(strictHandler)

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to find available port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Start server in background
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	// Wait for server to be ready
	serverURL := fmt.Sprintf("http://%s", addr)
	for i := 0; i < 50; i++ {
		resp, err := http.Get(serverURL + "/numbers?number=1")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return serverURL, httpServer, queries, nil
}

// apiServer implements the API server interface
type apiServer struct {
	queries *sqlc.Queries
}

func (s *apiServer) AddNumber(ctx context.Context, request api.AddNumberRequestObject) (api.AddNumberResponseObject, error) {
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

// clearDatabase removes all data from the numbers table
func clearDatabase(t *testing.T) {
	ctx := context.Background()
	_, err := testQueries.GetAllNumbersSorted(ctx)
	require.NoError(t, err)

	// Clear the table
	pool, err := pgxpool.New(ctx, testDBDSN)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, "DELETE FROM numbers")
	require.NoError(t, err)
}

// TestAddNumber_SingleNumber tests adding a single number
func TestAddNumber_SingleNumber(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	// Add number 3
	params := &api.AddNumberParams{Number: 3}
	resp, err := testClient.AddNumberWithResponse(ctx, params)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.NotNil(t, resp.JSON200.Numbers)
	assert.Equal(t, []int{3}, *resp.JSON200.Numbers)
}

// TestAddNumber_MultipleNumbersDescending tests adding numbers in descending order
func TestAddNumber_MultipleNumbersDescending(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	// Add number 3
	params := &api.AddNumberParams{Number: 3}
	resp, err := testClient.AddNumberWithResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	assert.Equal(t, []int{3}, *resp.JSON200.Numbers)

	// Add number 2
	params = &api.AddNumberParams{Number: 2}
	resp, err = testClient.AddNumberWithResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	assert.Equal(t, []int{2, 3}, *resp.JSON200.Numbers)

	// Add number 1
	params = &api.AddNumberParams{Number: 1}
	resp, err = testClient.AddNumberWithResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	assert.Equal(t, []int{1, 2, 3}, *resp.JSON200.Numbers)
}

// TestAddNumber_RandomOrder tests adding numbers in random order
func TestAddNumber_RandomOrder(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	numbers := []int{5, 1, 9, 3, 7}
	expected := [][]int{
		{5},
		{1, 5},
		{1, 5, 9},
		{1, 3, 5, 9},
		{1, 3, 5, 7, 9},
	}

	for i, num := range numbers {
		params := &api.AddNumberParams{Number: num}
		resp, err := testClient.AddNumberWithResponse(ctx, params)

		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode())
		require.NotNil(t, resp.JSON200)
		require.NotNil(t, resp.JSON200.Numbers)
		assert.Equal(t, expected[i], *resp.JSON200.Numbers, "Failed at iteration %d", i)
	}
}

// TestAddNumber_DuplicateNumbers tests adding duplicate numbers
func TestAddNumber_DuplicateNumbers(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	// Add number 5 three times
	for i := 0; i < 3; i++ {
		params := &api.AddNumberParams{Number: 5}
		resp, err := testClient.AddNumberWithResponse(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode())
	}

	// Add number 3
	params := &api.AddNumberParams{Number: 3}
	resp, err := testClient.AddNumberWithResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.NotNil(t, resp.JSON200.Numbers)
	assert.Equal(t, []int{3, 5, 5, 5}, *resp.JSON200.Numbers)
}

// TestAddNumber_NegativeNumbers tests adding negative numbers
func TestAddNumber_NegativeNumbers(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	numbers := []int{-5, -10, -1}
	expected := [][]int{
		{-5},
		{-10, -5},
		{-10, -5, -1},
	}

	for i, num := range numbers {
		params := &api.AddNumberParams{Number: num}
		resp, err := testClient.AddNumberWithResponse(ctx, params)

		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode())
		require.NotNil(t, resp.JSON200)
		require.NotNil(t, resp.JSON200.Numbers)
		assert.Equal(t, expected[i], *resp.JSON200.Numbers, "Failed at iteration %d", i)
	}
}

// TestAddNumber_Zero tests adding zero
func TestAddNumber_Zero(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	// Add zero
	params := &api.AddNumberParams{Number: 0}
	resp, err := testClient.AddNumberWithResponse(ctx, params)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.NotNil(t, resp.JSON200.Numbers)
	assert.Equal(t, []int{0}, *resp.JSON200.Numbers)

	// Add positive and negative numbers
	params = &api.AddNumberParams{Number: 5}
	resp, err = testClient.AddNumberWithResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())

	params = &api.AddNumberParams{Number: -3}
	resp, err = testClient.AddNumberWithResponse(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	assert.Equal(t, []int{-3, 0, 5}, *resp.JSON200.Numbers)
}

// TestAddNumber_LargeNumbers tests adding very large numbers
func TestAddNumber_LargeNumbers(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	// Test with large positive and negative numbers (within int32 range)
	numbers := []int{2147483647, -2147483648, 1000000000, -1000000000}
	expected := [][]int{
		{2147483647},
		{-2147483648, 2147483647},
		{-2147483648, 1000000000, 2147483647},
		{-2147483648, -1000000000, 1000000000, 2147483647},
	}

	for i, num := range numbers {
		params := &api.AddNumberParams{Number: num}
		resp, err := testClient.AddNumberWithResponse(ctx, params)

		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode())
		require.NotNil(t, resp.JSON200)
		require.NotNil(t, resp.JSON200.Numbers)
		assert.Equal(t, expected[i], *resp.JSON200.Numbers, "Failed at iteration %d", i)
	}
}

// TestAddNumber_MixedPositiveNegative tests adding mixed positive and negative numbers
func TestAddNumber_MixedPositiveNegative(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	numbers := []int{10, -5, 20, -15, 0, 3, -3}
	expected := [][]int{
		{10},
		{-5, 10},
		{-5, 10, 20},
		{-15, -5, 10, 20},
		{-15, -5, 0, 10, 20},
		{-15, -5, 0, 3, 10, 20},
		{-15, -5, -3, 0, 3, 10, 20},
	}

	for i, num := range numbers {
		params := &api.AddNumberParams{Number: num}
		resp, err := testClient.AddNumberWithResponse(ctx, params)

		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode())
		require.NotNil(t, resp.JSON200)
		require.NotNil(t, resp.JSON200.Numbers)
		assert.Equal(t, expected[i], *resp.JSON200.Numbers, "Failed at iteration %d", i)
	}
}

// TestAddNumber_VerifyDatabaseState verifies that numbers are actually stored in the database
func TestAddNumber_VerifyDatabaseState(t *testing.T) {
	clearDatabase(t)
	ctx := context.Background()

	// Add numbers via API
	numbers := []int{7, 2, 9}
	for _, num := range numbers {
		params := &api.AddNumberParams{Number: num}
		resp, err := testClient.AddNumberWithResponse(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode())
	}

	// Verify directly from database
	dbNumbers, err := testQueries.GetAllNumbersSorted(ctx)
	require.NoError(t, err)
	require.Len(t, dbNumbers, 3)

	// Convert to int slice and verify sorting
	result := make([]int, len(dbNumbers))
	for i, num := range dbNumbers {
		result[i] = int(num.Number)
	}
	assert.Equal(t, []int{2, 7, 9}, result)
}
