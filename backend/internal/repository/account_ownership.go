package repository

import (
	"context"
	"database/sql"
)

func (r *accountRepository) CanScheduleAccountForUser(ctx context.Context, accountID, userID int64) (bool, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT account_user_can_schedule($1, $2)`, accountID, userID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, sql.ErrNoRows
	}
	var allowed bool
	if err := rows.Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, rows.Err()
}
