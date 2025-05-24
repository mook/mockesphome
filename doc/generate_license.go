package main

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"text/template"

	"github.com/go-git/go-git/v5"
	"github.com/google/licenseclassifier"
	"github.com/google/licenseclassifier/serializer"
	"golang.org/x/tools/go/packages"
)

//go:embed notice.txt.gotmpl
var licenseTemplate string

type dependencyClass int

const detectionThreshold = 0.85

const (
	dependencyClassMain = dependencyClass(iota)
	dependencyClassDirect
	dependencyClassIndirect
)

type licenseItem struct {
	Name        string
	Version     string
	Class       dependencyClass
	Directory   string // Absolute path to the directory for this item
	LicenseFile string // Absolute path to the license file.
	LicenseId   string // License identifier
}

// Find the license file for the item.
func (i *licenseItem) FindLicenseFile() error {
	if i.LicenseFile != "" {
		return nil // Has override.
	}
	entries, err := os.ReadDir(i.Directory)
	if err != nil {
		return err
	}

	matchers := []*regexp.Regexp{
		regexp.MustCompile(`^(?i)li[cs]en[cs]es?`),
		regexp.MustCompile(`^(?i)legal`),
		regexp.MustCompile(`^(?i)copy(?:right|left|ing)`),
		regexp.MustCompile(`^(?i)l?gpl`),
		regexp.MustCompile(`^(?i)(?:bsd|mit|apache)`),
		regexp.MustCompile(`(?i)li[cs]en[cs]es?`),
		regexp.MustCompile(`(?i)legal`),
		regexp.MustCompile(`(?i)copy(?:right|left|ing)`),
		regexp.MustCompile(`(?i)l?gpl`),
		regexp.MustCompile(`(?i)(?:bsd|mit|apache)`),
	}

	type candidate struct {
		Name  string
		Index int
	}
	var candidates []candidate
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			index := slices.IndexFunc(matchers, func(re *regexp.Regexp) bool {
				return re.MatchString(entry.Name())
			})
			if index > -1 {
				candidates = append(candidates, candidate{
					Name:  entry.Name(),
					Index: index,
				})
			}
		}
	}
	if len(candidates) < 1 {
		return fmt.Errorf("failed to find license file for %s (%s)", i.Name, i.Directory)
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		return cmp.Compare(a.Index, b.Index)
	})
	i.LicenseFile = candidates[0].Name
	return nil
}

func (i *licenseItem) Identify(classifier *licenseclassifier.License) error {
	contents, err := os.ReadFile(filepath.Join(i.Directory, i.LicenseFile))
	if err != nil {
		return err
	}
	matches := classifier.MultipleMatch(string(contents), true)
	if len(matches) < 1 {
		return fmt.Errorf("failed to detect license for %s", i.Name)
	}
	i.LicenseId = matches[0].Name
	return nil
}

func generateLicenseNotice(ctx context.Context, writer io.Writer) error {
	packages := map[string]*licenseItem{}
	if err := parseGoPackages(ctx, packages); err != nil {
		return err
	}
	if err := parseGitSubmodules(ctx, packages); err != nil {
		return err
	}

	items := slices.Collect(maps.Values(packages))

	slices.SortFunc(items, func(a, b *licenseItem) int {
		if a.Class != b.Class {
			return cmp.Compare(a.Class, b.Class)
		}
		return cmp.Compare(a.Name, b.Name)
	})

	var licenseArchive bytes.Buffer
	licensesDir, err := licenseclassifier.ReadLicenseDir()
	if err != nil {
		return fmt.Errorf("failed to read license data: %w", err)
	}
	var licenseNames []string
	for _, licenseInfo := range licensesDir {
		licenseNames = append(licenseNames, licenseInfo.Name())
	}
	if err := serializer.ArchiveLicenses(licenseNames, &licenseArchive); err != nil {
		return fmt.Errorf("failed to pre-process licenses: %w", err)
	}

	classifier, err := licenseclassifier.New(detectionThreshold, licenseclassifier.ArchiveBytes(licenseArchive.Bytes()))
	if err != nil {
		return fmt.Errorf("failed to create classifier: %w", err)
	}

	for _, mod := range items {
		if err := mod.FindLicenseFile(); err != nil {
			return err
		}
		if err := mod.Identify(classifier); err != nil {
			return err
		}
	}

	itemsByClass := map[dependencyClass][]*licenseItem{}
	for _, item := range items {
		itemsByClass[item.Class] = append(itemsByClass[item.Class], item)
	}

	tmpl, err := template.New("").Funcs(templateFunctions).Parse(licenseTemplate)
	if err != nil {
		return fmt.Errorf("failed to read license notice template: %w", err)
	}
	if err := tmpl.Execute(writer, itemsByClass); err != nil {
		return fmt.Errorf("failed to write license notice: %w", err)
	}

	return nil
}

// Parse the go packages for this project, updating the map.
func parseGoPackages(ctx context.Context, seen map[string]*licenseItem) error {
	config := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedModule}
	pkgs, err := packages.Load(config, "github.com/mook/mockesphome/...")
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}
	var errs []error
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("error loading packages: %w", errors.Join(errs...))
	}

	for _, pkg := range pkgs {
		if err := parseGoPackage(ctx, seen, pkg); err != nil {
			return err
		}
	}

	return nil
}

// Given a single go package, insert its license item to the map and recursively
// parse its dependencies.
func parseGoPackage(ctx context.Context, seen map[string]*licenseItem, pkg *packages.Package) error {
	if pkg.Module == nil {
		return nil // Skip standard library imports
	}
	class := dependencyClassIndirect
	if pkg.Module.Main {
		class = dependencyClassMain
	}
	if existing, ok := seen[pkg.Module.Path]; ok {
		if existing.Class < class {
			class = existing.Class
		}
	}
	seen[pkg.Module.Path] = &licenseItem{
		Name:      pkg.Module.Path,
		Version:   pkg.Module.Version,
		Class:     class,
		Directory: pkg.Module.Dir,
	}

	for _, i := range pkg.Imports {
		if i.Module == nil {
			continue // Skip standard library imports
		}
		if err := parseGoPackage(ctx, seen, i); err != nil {
			return err
		}
		if pkg.Module.Main && seen[i.Module.Path].Class > dependencyClassDirect {
			seen[i.Module.Path].Class = dependencyClassDirect
		}
	}
	return nil
}

func parseGitSubmodules(ctx context.Context, seen map[string]*licenseItem) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	repo, err := git.PlainOpen(cwd)
	if err != nil {
		return fmt.Errorf("failed to open git repository: %w", err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to open git worktree: %w", err)
	}
	submodules, err := worktree.Submodules()
	if err != nil {
		return fmt.Errorf("failed to open git submodules: %w", err)
	}
	for _, submodule := range submodules {
		if repo, err = submodule.Repository(); err != nil {
			return fmt.Errorf("failed to open git submodule repository: %w", err)
		}
		if worktree, err = repo.Worktree(); err != nil {
			return fmt.Errorf("failed to open git submodule worktree: %w", err)
		}
		head, err := repo.Head()
		if err != nil {
			return fmt.Errorf("failed to get git submodule head: %w", err)
		}
		seen[submodule.Config().URL] = &licenseItem{
			Name:      submodule.Config().URL,
			Version:   fmt.Sprintf("%s (%s)", head.Name().String(), head.Hash().String()),
			Class:     dependencyClassDirect,
			Directory: worktree.Filesystem.Root(),
		}
	}
	return nil
}
