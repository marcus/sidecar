package docdrift

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// CodeFeature represents a feature extracted from code.
type CodeFeature struct {
	Name        string // Function, type, interface name
	Type        string // "function", "type", "interface", "method"
	Signature   string // Full signature for functions
	Package     string // Package name
	SourceFile  string // Path to source file
	IsExported  bool   // Is it publicly exported?
}

// CodeAnalyzer extracts features from Go code.
type CodeAnalyzer struct {
	RootDir string
	Features []CodeFeature
}

// NewCodeAnalyzer creates a new code analyzer.
func NewCodeAnalyzer(rootDir string) *CodeAnalyzer {
	return &CodeAnalyzer{
		RootDir:  rootDir,
		Features: []CodeFeature{},
	}
}

// AnalyzePackage extracts exported items from a Go package.
func (ca *CodeAnalyzer) AnalyzePackage(packagePath string) error {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, packagePath, nil, parser.AllErrors)
	if err != nil {
		return err
	}

	for pkgName, pkg := range pkgs {
		for fileName, f := range pkg.Files {
			ca.analyzeFile(f, pkgName, fileName)
		}
	}

	return nil
}

func (ca *CodeAnalyzer) analyzeFile(f *ast.File, pkgName, fileName string) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			ca.analyzeGenDecl(d, pkgName, fileName)
		case *ast.FuncDecl:
			ca.analyzeFuncDecl(d, pkgName, fileName)
		}
	}
}

func (ca *CodeAnalyzer) analyzeGenDecl(d *ast.GenDecl, pkgName, fileName string) {
	switch d.Tok {
	case token.TYPE:
		for _, spec := range d.Specs {
			if typeSpec, ok := spec.(*ast.TypeSpec); ok && ast.IsExported(typeSpec.Name.Name) {
				ca.Features = append(ca.Features, CodeFeature{
					Name:       typeSpec.Name.Name,
					Type:       "type",
					Package:    pkgName,
					SourceFile: filepath.Base(fileName),
					IsExported: true,
				})
			}
		}
	case token.CONST, token.VAR:
		for _, spec := range d.Specs {
			if valSpec, ok := spec.(*ast.ValueSpec); ok {
				for _, name := range valSpec.Names {
					if ast.IsExported(name.Name) {
						ca.Features = append(ca.Features, CodeFeature{
							Name:       name.Name,
							Type:       strings.ToLower(d.Tok.String()),
							Package:    pkgName,
							SourceFile: filepath.Base(fileName),
							IsExported: true,
						})
					}
				}
			}
		}
	}
}

func (ca *CodeAnalyzer) analyzeFuncDecl(d *ast.FuncDecl, pkgName, fileName string) {
	if !ast.IsExported(d.Name.Name) {
		return
	}

	featureType := "function"
	if d.Recv != nil {
		featureType = "method"
	}

	signature := ca.extractFuncSignature(d)
	ca.Features = append(ca.Features, CodeFeature{
		Name:       d.Name.Name,
		Type:       featureType,
		Signature:  signature,
		Package:    pkgName,
		SourceFile: filepath.Base(fileName),
		IsExported: true,
	})
}

// extractFuncSignature builds a function signature from AST.
func (ca *CodeAnalyzer) extractFuncSignature(d *ast.FuncDecl) string {
	var sb strings.Builder

	// Receiver (for methods)
	if d.Recv != nil {
		sb.WriteString("(")
		for i, field := range d.Recv.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(ca.typeToString(field.Type))
		}
		sb.WriteString(") ")
	}

	sb.WriteString(d.Name.Name)
	sb.WriteString("(")

	// Parameters
	if d.Type.Params != nil {
		for i, field := range d.Type.Params.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			if len(field.Names) > 0 {
				for j, name := range field.Names {
					if j > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(name.Name)
					sb.WriteString(" ")
				}
			}
			sb.WriteString(ca.typeToString(field.Type))
		}
	}
	sb.WriteString(")")

	// Return types
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		sb.WriteString(" (")
		for i, field := range d.Type.Results.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(ca.typeToString(field.Type))
		}
		sb.WriteString(")")
	}

	return sb.String()
}

// typeToString converts an AST type to a string representation.
func (ca *CodeAnalyzer) typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", ca.typeToString(t.X), t.Sel.Name)
	case *ast.StarExpr:
		return "*" + ca.typeToString(t.X)
	case *ast.ArrayType:
		return "[]" + ca.typeToString(t.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", ca.typeToString(t.Key), ca.typeToString(t.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	default:
		return "unknown"
	}
}

// ExtractPluginNames extracts plugin IDs from the plugins directory.
func (ca *CodeAnalyzer) ExtractPluginNames() ([]string, error) {
	pluginDir := filepath.Join(ca.RootDir, "internal", "plugins")
	entries, err := filepath.Glob(pluginDir + "/*")
	if err != nil {
		return nil, err
	}

	var plugins []string
	for _, entry := range entries {
		// Only include directories that are actual plugins
		name := filepath.Base(entry)
		// Skip hidden files
		if !strings.HasPrefix(name, ".") {
			plugins = append(plugins, name)
		}
	}

	return plugins, nil
}
