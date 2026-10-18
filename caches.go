package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"godrome/internal/cache"
)

// appCaches owns the regenerable disk cache. Covers and metadata have
// separate budgets so browsing many albums cannot evict the library
// metadata that offline mode needs.
type appCaches struct {
	covers *cache.Store
	meta   *cache.Store
}

func openCaches(limitMB int) (*appCaches, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	// Earlier versions wrote MPRIS covers here with no index or limit.
	_ = os.RemoveAll(filepath.Join(root, "godrome", "covers"))
	base := filepath.Join(root, "godrome", "cache")
	coverLimit, metaLimit := cacheBudgets(limitMB)
	covers, err := cache.Open(filepath.Join(base, "covers"), coverLimit)
	if err != nil {
		return nil, err
	}
	meta, err := cache.Open(filepath.Join(base, "meta"), metaLimit)
	if err != nil {
		covers.Close()
		return nil, err
	}
	return &appCaches{covers: covers, meta: meta}, nil
}

// cacheBudgets splits one user-facing limit 7:1, with at least 16 MiB for
// metadata.
func cacheBudgets(limitMB int) (covers, meta int64) {
	if limitMB <= 0 {
		limitMB = 256
	}
	total := int64(limitMB) << 20
	meta = total / 8
	if meta < 16<<20 {
		meta = 16 << 20
	}
	if meta > total {
		meta = total
	}
	return total - meta, meta
}

// The client keys covers with a "cover|" prefix and metadata with "meta|",
// so one BlobCache can route to the right budget.
func (c *appCaches) store(key string) *cache.Store {
	if strings.HasPrefix(key, "cover|") {
		return c.covers
	}
	return c.meta
}

func (c *appCaches) Get(key string) ([]byte, bool)     { return c.store(key).Get(key) }
func (c *appCaches) Path(key string) (string, bool)    { return c.store(key).Path(key) }
func (c *appCaches) Put(key string, data []byte) error { return c.store(key).Put(key, data) }
func (c *appCaches) Stats() (used, limit int64) {
	cu, cl := c.covers.Stats()
	mu, ml := c.meta.Stats()
	return cu + mu, cl + ml
}

func (c *appCaches) SetLimitMB(limitMB int) {
	coverLimit, metaLimit := cacheBudgets(limitMB)
	c.covers.SetLimit(coverLimit)
	c.meta.SetLimit(metaLimit)
}

func (c *appCaches) Clear() error {
	return errors.Join(c.covers.Clear(), c.meta.Clear())
}

func (c *appCaches) Close() error {
	return errors.Join(c.covers.Close(), c.meta.Close())
}
