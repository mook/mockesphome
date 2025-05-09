// Package component
package components

import (
	"context"
)

// Common interface for components; each component must implement this.
type Component interface {
	// Get the ID of the component.
	ID() string
	// Get the components this componet depends on.
	Dependencies() []string
	// Configure the component; the loader function should be called to parse the
	// configuration, passing in a structure to decode from the YAML.
	Configure(ctx context.Context, load func(any) error) error
	// Start the component, likely in a goroutine.  The component should shut
	// down when the context is done.
	Start(ctx context.Context) error
}

var ComponentRegistry = make(map[string]Component)
