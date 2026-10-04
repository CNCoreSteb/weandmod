// Package trainer abstracts trainer search backends.
package trainer

import "context"

// Trainer is a single search result.
type Trainer struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Source string `json:"source"`
}

// Source searches a trainer provider.
type Source interface {
	Name() string
	Search(ctx context.Context, query string) ([]Trainer, error)
}

// DefaultSources returns the enabled search backends.
func DefaultSources() []Source {
	return []Source{&Fling{}}
}
