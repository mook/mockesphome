// Command doc generates documentation for the configuration
package main

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"text/template"

	"slices"

	"github.com/mook/mockesphome/components"
	_ "github.com/mook/mockesphome/load"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

var (
	//go:embed doc.md.gotmpl
	docTemplate string
)

type configItem struct {
	Type        string
	Description string
}
type component struct {
	Name        string                // The name of the component
	Description string                // The component documentation
	Config      map[string]configItem // Configuration items for this component
}

func generateDocs(ctx context.Context, writer io.Writer) error {
	tmpl, err := template.New("").Funcs(templateFunctions).Parse(docTemplate)
	if err != nil {
		return err
	}

	pkgs, err := loadPackages()
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}
	slog.DebugContext(ctx, "loaded packages", "packages", pkgs)

	var components []*component
	for _, pkg := range pkgs {
		c, err := parseComponent(ctx, pkg)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", pkg.PkgPath, err)
		}
		components = append(components, c)
	}
	slices.SortFunc(components, func(a, b *component) int {
		return cmp.Compare(a.Name, b.Name)
	})

	if err := tmpl.Execute(writer, components); err != nil {
		return err
	}
	return nil
}

func loadPackages() ([]*packages.Package, error) {
	var pkgPaths []string
	for component := range components.Enumerate() {
		typ := reflect.TypeOf(component)
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		pkgPaths = append(pkgPaths, typ.PkgPath())
	}
	config := &packages.Config{Mode: packages.NeedName | packages.NeedSyntax | packages.NeedDeps | packages.NeedTypes}
	return packages.Load(config, pkgPaths...)
}

// Parse a component package, returning its name and configuration items.
func parseComponent(ctx context.Context, pkg *packages.Package) (*component, error) {
	if len(pkg.Errors) > 0 {
		var errs []error
		for _, err := range pkg.Errors {
			errs = append(errs, err)
		}
		return nil, fmt.Errorf("package load failure: %w", errors.Join(errs...))
	}
	if pkg.Types == nil {
		return nil, fmt.Errorf("failed to load package types")
	}

	result := component{
		Name: pkg.PkgPath[strings.LastIndex(pkg.PkgPath, "/")+1:],
	}

	for _, file := range pkg.Syntax {
		if file.Doc != nil {
			result.Description += file.Doc.Text() + "\n"
		}
	}

	configType := pkg.Types.Scope().Lookup("Configuration")
	if configType != nil {
		config, err := genConfigDocs(configType, pkg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse config for %s: %w", result.Name, err)
		}
		result.Config = config
	}
	return &result, nil
}

// Given the type object for the declaration of the config object, emit its
// documentation.
func genConfigDocs(object types.Object, pkg *packages.Package) (map[string]configItem, error) {
	pos := object.Pos()
	for _, file := range pkg.Syntax {
		if !(file.FileStart <= pos && pos < file.FileEnd) {
			continue // Not in this file
		}
		path, _ := astutil.PathEnclosingInterval(file, pos, pos)
		ident := getNodeOfType[*ast.Ident](path)
		if ident == nil {
			return nil, fmt.Errorf("failed to get ident for %s", object.Id())
		}
		typeSpec := getNodeOfType[*ast.TypeSpec](path)
		if typeSpec == nil {
			return nil, fmt.Errorf("failed to find TypeSpec for %s", ident.Name)
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return nil, fmt.Errorf("failed to convert TypeSpect to StructType")
		}
		return parseConfigProperties(structType, nil)
	}
	return nil, fmt.Errorf("failed to find file for %+v", object)
}

func getNodeOfType[T ast.Node](path []ast.Node) T {
	for _, node := range path {
		if t, ok := node.(T); ok {
			return t
		}
	}
	return *new(T) // Returns nil
}

// Given an expression, remove all layers of parentheses.
func unParen(expr ast.Expr) ast.Expr {
	for {
		e, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = e.X
	}
}

// Given a StructType of configuration, return the configuration items for
// templating.  The prefix is pre-pended to the field names, for use with nested
// structures.
func parseConfigProperties(structType *ast.StructType, prefix []string) (map[string]configItem, error) {
	result := make(map[string]configItem)
	for _, field := range structType.Fields.List {
		fieldName := ""
		if ident, ok := unParen(field.Type).(*ast.Ident); ok {
			fieldName = strings.ToLower(ident.Name)
		}
		for _, name := range field.Names {
			if name != nil {
				fieldName = strings.ToLower(name.Name)
				break
			}
		}
		if field.Tag != nil && field.Tag.Kind == token.STRING {
			tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
			for _, tagName := range []string{"yaml", "json"} {
				if t := tag.Get(tagName); t != "" {
					if name, _, _ := strings.Cut(t, ","); name != "" {
						fieldName = name
					}
					break
				}
			}
		}
		fullName := append(slices.Clone(prefix), fieldName)
		comment := "(no documentation)"
		if field.Comment != nil {
			comment = strings.TrimSpace(field.Comment.Text())
		}
		if field.Doc != nil {
			comment = strings.TrimSpace(field.Doc.Text())
		}
		result[strings.Join(fullName, ".")] = configItem{
			Type:        fmt.Sprintf("%s", unParen(field.Type)),
			Description: comment,
		}
		if nestedStruct, ok := unParen(field.Type).(*ast.StructType); ok {
			childItems, err := parseConfigProperties(nestedStruct, fullName)
			if err != nil {
				return nil, err
			}
			maps.Copy(result, childItems)
		}
	}
	return result, nil
}
