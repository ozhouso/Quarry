package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// we dont want a 135k line json after the very first run so each list is capped
const maxListed = 5000

// Changeset is one extraction's worth of changes, saved so `quarry diff` can show it later
type Changeset struct {
	Time      string   `json:"time"`
	AddedN    int      `json:"addedN"`
	ModifiedN int      `json:"modifiedN"`
	RemovedN  int      `json:"removedN"`
	Added     []string `json:"added,omitempty"`
	Modified  []string `json:"modified,omitempty"`
	Removed   []string `json:"removed,omitempty"`
}

func (r *Changeset) Total() int { return r.AddedN + r.ModifiedN + r.RemovedN }

func (r *Changeset) addAdded(f string) {
	r.AddedN++
	if len(r.Added) < maxListed {
		r.Added = append(r.Added, f)
	}
}
func (r *Changeset) addModified(f string) {
	r.ModifiedN++
	if len(r.Modified) < maxListed {
		r.Modified = append(r.Modified, filepath.ToSlash(f))
	}
}
func (r *Changeset) addRemoved(f string) {
	r.RemovedN++
	if len(r.Removed) < maxListed {
		r.Removed = append(r.Removed, filepath.ToSlash(f))
	}
}

func (r *Changeset) finalize() {
	r.Time = time.Now().Format(time.RFC3339)
	sort.Strings(r.Added)
	sort.Strings(r.Modified)
	sort.Strings(r.Removed)
}

const changesName = ".quarry-changes.json"
const keepRuns = 10

// LoadRuns returns saved runs, newest first
func LoadRuns(dest string) ([]Changeset, error) {
	data, err := os.ReadFile(filepath.Join(dest, changesName))
	if err != nil {
		return nil, err
	}
	var runs []Changeset
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func appendRun(dest string, r Changeset) error {
	runs, _ := LoadRuns(dest)
	runs = append([]Changeset{r}, runs...)
	if len(runs) > keepRuns {
		runs = runs[:keepRuns]
	}
	data, err := json.Marshal(runs)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, changesName), data, 0o644)
}
