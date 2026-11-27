package taskactions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresQueue struct {
	Pool                             *pgxpool.Pool
	ClaimSQL, CompleteSQL, ExpireSQL string
}

func (q PostgresQueue) ClaimTask(ctx context.Context, user int64) (*Job, error) {
	if _, err := q.Pool.Exec(ctx, q.ExpireSQL); err != nil {
		return nil, err
	}
	var job Job
	err := q.Pool.QueryRow(ctx, q.ClaimSQL, user, uuid.NewString()).Scan(&job.ID, &job.LeaseToken, &job.Text, &job.ExpiresAt, &job.Date, &job.OperationKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (q PostgresQueue) CompleteTask(ctx context.Context, user, id int64, token string, success bool) error {
	tag, err := q.Pool.Exec(ctx, q.CompleteSQL, id, user, token, success)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
