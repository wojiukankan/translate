//go:build windows || sqlitego

package translation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// SQLiteCache keeps successful translations across app restarts.
type SQLiteCache struct{ db *sql.DB }

func OpenSQLiteCache(ctx context.Context, path string) (*SQLiteCache, error) {
	if path != ":memory:" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
		if err != nil {
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}
	// Match the existing MyGo SQLite connection's defaults and serialization.
	db, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS translations (cache_key TEXT PRIMARY KEY, result TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteCache{db: db}, nil
}

func (c *SQLiteCache) Get(ctx context.Context, key string) (string, bool, error) {
	var result any
	err := c.db.QueryRowContext(ctx, `SELECT result FROM translations WHERE cache_key = ?`, key).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, ok := result.(string)
	if !ok {
		return "", false, fmt.Errorf("缓存数据格式错误")
	}
	return value, true, nil
}

func (c *SQLiteCache) Put(ctx context.Context, key, result string) error {
	_, err := c.db.ExecContext(ctx, `INSERT INTO translations (cache_key, result) VALUES (?, ?) ON CONFLICT(cache_key) DO UPDATE SET result = excluded.result`, key, result)
	return err
}

func (c *SQLiteCache) Close() error { return c.db.Close() }
