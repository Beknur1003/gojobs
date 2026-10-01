package ats

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Cache remembers which listing versions already had their descriptions
// downloaded, so a board is read as a light list of titles every day and only
// new or edited roles cost a full fetch. It is a local file that can be lost:
// an empty cache just means one heavier run.
type Cache struct {
	path  string
	rules string // version of the rules that judged the cached roles

	mu   sync.Mutex
	seen map[string]map[string]string // feed -> listing id -> version
}

type cacheFile struct {
	Rules string                       `json:"rules"`
	Seen  map[string]map[string]string `json:"seen"`
}

// LoadCache reads the cache, or starts empty when it was written under other
// rules: a role the old rules rejected must be judged again by the new ones.
func LoadCache(path, rules string) (*Cache, error) {
	c := &Cache{path: path, rules: rules, seen: map[string]map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ats.LoadCache: %w", err)
	}
	var f cacheFile
	// A corrupt or outdated cache only costs bandwidth; start over.
	if json.Unmarshal(raw, &f) == nil && f.Rules == rules && f.Seen != nil {
		c.seen = f.Seen
	}
	return c, nil
}

func (c *Cache) Save() error {
	c.mu.Lock()
	raw, err := json.Marshal(cacheFile{Rules: c.rules, Seen: c.seen})
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("ats.Cache.Save: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("ats.Cache.Save: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("ats.Cache.Save: %w", err)
	}
	return os.Rename(tmp, c.path)
}

// Has reports whether this version of the listing was fetched before.
func (c *Cache) Has(feed, id, version string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.seen[feed][id]
	return ok && v == version
}

// Replace records the listings a board currently has. Listings that left the
// board are forgotten, which keeps the file the size of the open roles.
func (c *Cache) Replace(feed string, versions map[string]string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(versions) == 0 {
		delete(c.seen, feed)
		return
	}
	c.seen[feed] = versions
}
