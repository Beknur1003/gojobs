package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

// LoadBoards reads the discovery registry: the last check of every company
// board that was ever probed, keyed by feed name ("greenhouse:stripe").
func LoadBoards(path string) (map[string]models.BoardStatus, error) {
	out := map[string]models.BoardStatus{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store.LoadBoards: %w", err)
	}
	var dto map[string]boardDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, fmt.Errorf("store.LoadBoards: parse %s: %w", path, err)
	}
	for k, b := range dto {
		out[k] = models.BoardStatus{Checked: b.Checked, OK: b.OK, Postings: b.Postings, Go: b.Go, Fails: b.Fails, Error: b.Error}
	}
	return out, nil
}

// SaveBoards writes the registry with one board per line, so the daily git
// diff shows exactly which boards were checked.
func SaveBoards(path string, boards map[string]models.BoardStatus) error {
	keys := make([]string, 0, len(boards))
	for k := range boards {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		s := boards[k]
		key, _ := json.Marshal(k)
		val, err := json.Marshal(boardDTO{Checked: s.Checked.UTC().Truncate(time.Second), OK: s.OK, Postings: s.Postings, Go: s.Go, Fails: s.Fails, Error: s.Error})
		if err != nil {
			return fmt.Errorf("store.SaveBoards: %w", err)
		}
		b.WriteString(" ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(val)
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return writeAtomic(path, []byte(b.String()))
}

type boardDTO struct {
	Checked  time.Time `json:"checked"`
	OK       bool      `json:"ok"`
	Postings int       `json:"n,omitempty"`
	Go       int       `json:"go,omitempty"`
	Fails    int       `json:"fails,omitempty"`
	Error    string    `json:"err,omitempty"`
}

// LoadSeeds reads candidate boards to probe from dir/<kind>.txt, one slug per
// line; "#" starts a comment. Missing files mean no seeds of that kind.
func LoadSeeds(dir string, kinds []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, kind := range kinds {
		f, err := os.Open(filepath.Join(dir, kind+".txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("store.LoadSeeds: %w", err)
		}
		seen := map[string]bool{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line, _, _ := strings.Cut(sc.Text(), "#")
			if line = strings.TrimSpace(line); line != "" && !seen[line] {
				seen[line] = true
				out[kind] = append(out[kind], line)
			}
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("store.LoadSeeds %s: %w", kind, err)
		}
	}
	return out, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}
