package components

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"golang.org/x/sync/errgroup"
)

type Registry struct {
	registered map[string]Component
	enabled    map[string]Component
}

var (
	registry = &Registry{
		registered: make(map[string]Component),
		enabled:    make(map[string]Component),
	}
)

// Register a component into the component registry.  This should be called from
// the `init()` function of each component's package.
func Register(c Component) {
	id := c.ID()
	if _, ok := registry.registered[id]; ok {
		panic(fmt.Sprintf("component %q was registered twice", id))
	}
	registry.registered[id] = c
}

// Load the configuration from a reader; this initializes the component manager.
func LoadConfiguration(ctx context.Context, configFile io.Reader) error {
	decoder := yaml.NewDecoder(configFile, yaml.DisallowUnknownField())
	config := make(map[string]ast.Node)
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Loop and initialize default configuration for all dependencies.
	gotNewComponents := true
	for gotNewComponents {
		gotNewComponents = false
		for k := range config {
			c := registry.registered[k]
			if c == nil {
				slog.WarnContext(ctx, "Ignoring unsupported component", "id", k)
				continue
			}
			for _, dep := range c.Dependencies() {
				if _, ok := config[dep]; !ok {
					gotNewComponents = true
					config[dep] = nil
					slog.DebugContext(ctx, "auto-loading dependency", "component", k, "requires", dep)
				}
			}
		}
	}

	// Initialize each component
	for name, componentConfig := range config {
		c := registry.registered[name]
		if c == nil {
			continue // We already warned above.
		}
		slog.DebugContext(ctx, "configuring component", "component", name)
		err := c.Configure(ctx, func(input any) error {
			if componentConfig == nil {
				return nil // No configuration; stay with defaults.
			}
			return decoder.DecodeFromNodeContext(ctx, componentConfig, input)
		})
		if err != nil {
			return fmt.Errorf("failed to configure component %q: %w", name, err)
		}
		registry.enabled[name] = c
	}

	return nil
}

// Start the components.
func StartComponents(ctx context.Context) error {
	errGroup := errgroup.Group{}
	for _, component := range registry.enabled {
		errGroup.Go(func() error { return component.Start(ctx) })
	}
	if err := errGroup.Wait(); err != nil {
		return fmt.Errorf("failed to start components: %w", err)
	}
	return nil
}
