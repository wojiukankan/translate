//go:build !windows && !sqlitego

package translation

import (
	"context"
	"fmt"
	"os"

	"github.com/egoist/mygo/plugins/sqlite"
)

// SQLiteCache keeps successful translations across app restarts.
type SQLiteCache struct{ db *sqlite.DB }

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
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return nil, err
	}
	if _, err = db.Execute(ctx, `CREATE TABLE IF NOT EXISTS translations (cache_key TEXT PRIMARY KEY, result TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteCache{db: db}, nil
}

func (c *SQLiteCache) Get(ctx context.Context, key string) (string, bool, error) {
	rows, err := c.db.Query(ctx, `SELECT result FROM translations WHERE cache_key = ?`, key)
	if err != nil || len(rows.Values) == 0 {
		return "", false, err
	}
	if len(rows.Values[0]) != 1 {
		return "", false, fmt.Errorf("缓存数据格式错误")
	}
	value, ok := rows.Values[0][0].(string)
	if !ok {
		return "", false, fmt.Errorf("缓存数据格式错误")
	}
	return value, true, nil
}

func (c *SQLiteCache) Put(ctx context.Context, key, result string) error {
	_, err := c.db.Execute(ctx, `INSERT INTO translations (cache_key, result) VALUES (?, ?) ON CONFLICT(cache_key) DO UPDATE SET result = excluded.result`, key, result)
	return err
}

func (c *SQLiteCache) Close() error { return c.db.Close() }
