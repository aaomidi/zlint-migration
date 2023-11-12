package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/tools/go/ast/astutil"
)

var (
	pathToLints = flag.String("path", "", "Path to lints directory")
)

func main() {
	flag.Parse()
	err := filepath.WalkDir(*pathToLints, func(path string, d fs.DirEntry, err error) error {
		// Skip directories
		if d.IsDir() {
			return nil
		}
		// Skip non go files
		if filepath.Ext(path) != ".go" {
			return nil
		}
		return handleFile(path)
	})
	if err != nil {
		log.Fatalf("error when walking directory: %v", err)
	}
}

func handleFile(filePath string) error {
	oldSrc, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("unable to read file: %w", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, oldSrc, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("unable to parse file: %w", err)
	}

	var astutilErr error
	astutil.Apply(file, nil, func(cursor *astutil.Cursor) bool {
		node, ok := cursor.Node().(*ast.CallExpr)
		if !ok {
			// Not at init yet
			return true
		}
		if _, ok := node.Fun.(*ast.SelectorExpr); !ok {
			return false
		}
		// We don't want to touch non RegisterLint methods.
		if node.Fun.(*ast.SelectorExpr).Sel.Name != "RegisterLint" {
			return true
		}

		functionCall := &ast.SelectorExpr{
			X:   ast.NewIdent("lint"),
			Sel: ast.NewIdent("RegisterCertificateLint"),
		}

		metadataFields := &ast.CompositeLit{
			Type: &ast.SelectorExpr{
				X:   ast.NewIdent("lint"),
				Sel: ast.NewIdent("LintMetadata"),
			},
			Elts:   nil,
			Lbrace: 0,
			Rbrace: 0,
		}

		certificateLintField := []ast.Expr{
			&ast.KeyValueExpr{
				Key:   ast.NewIdent("LintMetadata"),
				Value: metadataFields,
				Colon: 0,
			},
			&ast.KeyValueExpr{
				Key:   ast.NewIdent("Lint"),
				Value: nil,
				Colon: 0,
			},
		}

		lintCertificateLint := &ast.CompositeLit{
			Type: &ast.SelectorExpr{
				X:   ast.NewIdent("lint"),
				Sel: ast.NewIdent("CertificateLint"),
			},
			Elts:   certificateLintField,
			Lbrace: 0,
			Rbrace: 0,
		}

		arguments := &ast.UnaryExpr{
			Op:    token.AND,
			X:     lintCertificateLint,
			OpPos: 0,
		}

		// Loop over existing metadata fields
		for _, elt := range node.Args[0].(*ast.UnaryExpr).X.(*ast.CompositeLit).Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				astutilErr = fmt.Errorf("CompositeLit field was was not KV: %v", spew.Sdump(elt))
				return false
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				astutilErr = fmt.Errorf("KV key was not Ident: %v", spew.Sdump(kv.Key))
				return false
			}
			switch key.Name {
			case "Lint":
				certificateLintField[1].(*ast.KeyValueExpr).Value = kv.Value
			default:
				// TODO: Remove the Location values from these elements, maybe?
				metadataFields.Elts = append(metadataFields.Elts, kv)
			}
		}

		newCallExpr := &ast.CallExpr{
			Fun:    functionCall,
			Args:   []ast.Expr{arguments},
			Lparen: 0,
			Rparen: 0,
		}
		cursor.Replace(newCallExpr)
		return true
	})
	if astutilErr != nil {
		return fmt.Errorf("issue manipulating AST: %w", astutilErr)
	}

	var buf bytes.Buffer
	cfg := printer.Config{
		Mode:     printer.UseSpaces | printer.TabIndent,
		Tabwidth: 8,
	}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return fmt.Errorf("error when printing AST: %w", err)
	}

	// Note: since the file already exists, the permissions won't actually be modified.
	if err := os.WriteFile(filePath, buf.Bytes(), 0777); err != nil {
		return fmt.Errorf("error when writing file: %w", err)
	}

	return nil
}
