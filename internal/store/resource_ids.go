package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Durable workflow and operation references must never point to replacement rows.
func reserveResourceID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table string) (int64, error) {
	switch table {
	case "pantry", "cooklog", "purchases", "recipes", "lists":
	default:
		return 0, fmt.Errorf("unsupported resource sequence")
	}
	var id int64
	err := q.QueryRowContext(ctx, `UPDATE resource_sequences SET value=MAX(value,(SELECT COALESCE(MAX(id),0) FROM `+table+`))+1 WHERE name=? RETURNING value`, table).Scan(&id)
	return id, err
}
