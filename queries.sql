-- name: InsertNumber :one
INSERT INTO numbers (number)
VALUES ($1)
RETURNING id, number;

-- name: GetAllNumbersSorted :many
SELECT id, number
FROM numbers
ORDER BY number ASC;
