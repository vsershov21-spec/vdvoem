package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) Setting(ctx context.Context, key string) string {
	var v string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v); errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
