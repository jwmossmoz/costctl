package cloudprice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// DefaultCacheTTL is how long a cached response is considered fresh.
// cloudprice.net updates daily, so 24 hours is the natural ceiling.
const DefaultCacheTTL = 24 * time.Hour

// cacheEntry is the on-disk representation of one cached HTTP response.
//
// Body is the raw response body; encoding/json renders []byte as base64,
// keeping the file human-inspectable for debugging.
type cacheEntry struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Status    int       `json:"status"`
	Body      []byte    `json:"body"`
}

// defaultCacheDir returns $XDG_CACHE_HOME/costctl, defaulting to ~/.cache/costctl.
func defaultCacheDir() (string, error) {
	if p := os.Getenv("COSTCTL_CACHE_DIR"); p != "" {
		return p, nil
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home dir: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "costctl"), nil
}

func (c *Client) cacheDir() (string, error) {
	if c.CacheDir != "" {
		return c.CacheDir, nil
	}
	return defaultCacheDir()
}

func cacheKey(url string) string {
	h := sha256.Sum256([]byte(cacheURL(url)))
	return hex.EncodeToString(h[:])
}

func cacheURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Del("subscription-key")
	u.RawQuery = q.Encode()
	return u.String()
}

// readCache returns (body, status, true) if a non-stale entry exists.
func (c *Client) readCache(url string) ([]byte, int, bool) {
	if !c.UseCache {
		return nil, 0, false
	}
	dir, err := c.cacheDir()
	if err != nil {
		return nil, 0, false
	}
	path := filepath.Join(dir, cacheKey(url)+".json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, false
	}
	if err != nil {
		return nil, 0, false
	}
	var e cacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, 0, false
	}
	ttl := c.CacheTTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	if time.Since(e.FetchedAt) > ttl {
		return nil, 0, false
	}
	return e.Body, e.Status, true
}

// writeCache persists a successful response. Errors are silently ignored —
// caching is best-effort and never blocks a successful fetch.
func (c *Client) writeCache(url string, status int, body []byte) {
	if !c.UseCache {
		return
	}
	if status != 200 {
		return
	}
	dir, err := c.cacheDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	e := cacheEntry{
		URL:       cacheURL(url),
		FetchedAt: time.Now(),
		Status:    status,
		Body:      body,
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(dir, cacheKey(url)+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
