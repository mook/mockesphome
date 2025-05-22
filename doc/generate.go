// Command doc generates documentation for the configuration
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"slices"

	"github.com/mook/mockesphome/components"
	_ "github.com/mook/mockesphome/load"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

var (
	//go:embed doc.md
	docPrefix string
	outPath   = flag.String("outPath", "-", "file to output to")
)

func run(ctx context.Context) error {
	flag.Parse()

	pkgs, err := loadPackages()
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}
	slog.DebugContext(ctx, "loaded packages", "packages", pkgs)

	var writer *os.File
	if *outPath == "-" {
		writer = os.Stdout
	} else {
		writer, err = os.Create(*outPath)
		if err != nil {
			return fmt.Errorf("failed to create output file %s: %w", *outPath, err)
		}
		defer writer.Close()
	}

	if _, err := writer.WriteString(docPrefix); err != nil {
		return err
	}

	for _, pkg := range pkgs {
		if err := genPackage(ctx, pkg, writer); err != nil {
			return fmt.Errorf("failed to generate %s: %w", pkg.PkgPath, err)
		}
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

func genPackage(ctx context.Context, pkg *packages.Package, writer io.Writer) error {
	if len(pkg.Errors) > 0 {
		var errs []error
		for _, err := range pkg.Errors {
			errs = append(errs, err)
		}
		return fmt.Errorf("package load failure: %w", errors.Join(errs...))
	}
	if pkg.Types == nil {
		return fmt.Errorf("failed to load package types")
	}

	var doc string
	for _, file := range pkg.Syntax {
		if file.Doc != nil {
			doc += file.Doc.Text()
		}
	}
	if strings.TrimSpace(doc) != "" {
		componentName := pkg.PkgPath[strings.LastIndex(pkg.PkgPath, "/")+1:]
		if _, err := fmt.Fprintf(writer, "\n### %s\n\n", componentName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "%s\n", doc); err != nil {
			return err
		}
	}

	configType := pkg.Types.Scope().Lookup("Configuration")
	if configType == nil {
		slog.DebugContext(ctx, "package has no Configuration", "package", pkg.PkgPath)
		return nil
	}
	slog.DebugContext(ctx, "got config type", "configuration", configType)
	return genConfigDocs(configType, pkg, writer)
}

// Given the type object for the declaration of the config object, emit its
// documentation.
func genConfigDocs(object types.Object, pkg *packages.Package, writer io.Writer) error {
	componentName := pkg.PkgPath[strings.LastIndex(pkg.PkgPath, "/")+1:]
	pos := object.Pos()
	for _, file := range pkg.Syntax {
		if !(file.FileStart <= pos && pos < file.FileEnd) {
			continue // Not in this file
		}
		path, _ := astutil.PathEnclosingInterval(file, pos, pos)
		ident := getNodeOfType[*ast.Ident](path)
		if ident == nil {
			return fmt.Errorf("failed to get ident for %s", object.Id())
		}
		typeSpec := getNodeOfType[*ast.TypeSpec](path)
		if typeSpec == nil {
			return fmt.Errorf("failed to find TypeSpec for %s", ident.Name)
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return fmt.Errorf("failed to convert TypeSpect to StructType")
		}
		if len(structType.Fields.List) == 0 {
			_, err := fmt.Fprintf(writer, "There is no configuration for `%s`.\n", componentName)
			return err
		}
		if _, err := fmt.Fprintf(writer, "Parameter | Description\n--- | ---\n"); err != nil {
			return err
		}
		return printConfigProperties(structType, nil, writer)
	}
	return fmt.Errorf("failed to find file for %+v", object)
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

// Given a StructType of configuration, print out the table rows describing the
// configuration properties.  The prefix is pre-pended to the field names, for
// use with nested structures.
func printConfigProperties(structType *ast.StructType, prefix []string, writer io.Writer) error {
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
		_, err := fmt.Fprintf(writer, "%s | %s\n", strings.Join(fullName, "."), comment)
		if err != nil {
			return err
		}
		if nestedStruct, ok := unParen(field.Type).(*ast.StructType); ok {
			if err := printConfigProperties(nestedStruct, fullName, writer); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	ctx := context.Background()

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to generate", "error", err)
		os.Exit(1)
	}
}
