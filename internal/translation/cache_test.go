package translation

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

func TestSQLiteCachePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "translations.sqlite")
	cache, err := OpenSQLiteCache(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(ctx, "key", "翻译结果"); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	cache, err = OpenSQLiteCache(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	value, found, err := cache.Get(ctx, "key")
	if err != nil || !found || value != "翻译结果" {
		t.Fatalf("after reopening: %q, found=%t, err=%v", value, found, err)
	}
	if _, found, err := cache.Get(ctx, "missing"); err != nil || found {
		t.Fatalf("missing key: found=%t, err=%v", found, err)
	}
	if err := cache.Put(ctx, "key", "更新译文"); err != nil {
		t.Fatal(err)
	}
	if value, found, err := cache.Get(ctx, "key"); err != nil || !found || value != "更新译文" {
		t.Fatalf("updated value = %q, found=%t, err=%v", value, found, err)
	}

	if runtime.GOOS != "darwin" {
		return // The compatibility fixture needs the macOS MyGo native module.
	}
	t.Run("reads a MyGo SQLite database", func(t *testing.T) {
		oldPath := filepath.Join(t.TempDir(), "旧缓存.sqlite")
		old, err := sqlite.Open(ctx, oldPath, sqlite.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = old.Execute(ctx, `CREATE TABLE translations (cache_key TEXT PRIMARY KEY, result TEXT NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		if _, err = old.Execute(ctx, `INSERT INTO translations (cache_key, result) VALUES (?, ?)`, "legacy-key", "历史译文"); err != nil {
			t.Fatal(err)
		}
		if _, err = old.Execute(ctx, `INSERT INTO translations (cache_key, result) VALUES (?, ?)`, "invalid-value", []byte("blob")); err != nil {
			t.Fatal(err)
		}
		if err = old.Close(); err != nil {
			t.Fatal(err)
		}
		newCache, err := OpenSQLiteCache(ctx, oldPath)
		if err != nil {
			t.Fatal(err)
		}
		if value, found, err := newCache.Get(ctx, "legacy-key"); err != nil || !found || value != "历史译文" {
			t.Fatalf("legacy value = %q, found=%t, err=%v", value, found, err)
		}
		if _, found, err := newCache.Get(ctx, "invalid-value"); err == nil || found || err.Error() != "缓存数据格式错误" {
			t.Fatalf("invalid cache value: found=%t, err=%v", found, err)
		}
		if err = newCache.Put(ctx, "legacy-key", "新版译文"); err != nil {
			t.Fatal(err)
		}
		if value, found, err := newCache.Get(ctx, "legacy-key"); err != nil || !found || value != "新版译文" {
			t.Fatalf("updated legacy value = %q, found=%t, err=%v", value, found, err)
		}
		if err := newCache.Close(); err != nil {
			t.Fatal(err)
		}
		old, err = sqlite.Open(ctx, oldPath, sqlite.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer old.Close()
		rows, err := old.Query(ctx, `SELECT result FROM translations WHERE cache_key = ?`, "legacy-key")
		if err != nil || len(rows.Values) != 1 || len(rows.Values[0]) != 1 || rows.Values[0][0] != "新版译文" {
			t.Fatalf("old driver read after update: values=%v, err=%v", rows.Values, err)
		}
	})
}
