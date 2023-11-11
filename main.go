package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"golang.org/x/tools/go/ast/astutil"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	pathToLints = flag.String("path", "", "Path to lints directory")
)

func main() {
	flag.Parse()
	err := filepath.WalkDir(*pathToLints, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		return handleFile(path)
	})
	if err != nil {
		panic(err)
	}
}

func handleFile(filePath string) error {
	oldSrc, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, oldSrc, parser.ParseComments)

	astutil.Apply(file, nil, func(cursor *astutil.Cursor) bool {
		node, ok := cursor.Node().(*ast.CallExpr)
		if !ok {
			// Not at init yet
			return true
		}
		if _, ok := node.Fun.(*ast.SelectorExpr); !ok {
			return false
		}
		if node.Fun.(*ast.SelectorExpr).Sel.Name != "RegisterLint" {
			return true
		}
		pos := node.Pos()
		fsetFile := fset.File(pos)

		fmt.Println(fsetFile)

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
				panic("CompositeLit fields were not KV")
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				panic("KV did not have Ident key")
			}
			switch key.Name {
			case "Lint":
				certificateLintField[1].(*ast.KeyValueExpr).Value = kv.Value
			default:
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
	var buf bytes.Buffer
	cfg := printer.Config{
		Mode:     printer.UseSpaces | printer.TabIndent,
		Tabwidth: 8,
	}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return err
	}
	if err := os.WriteFile(filePath, buf.Bytes(), 0777); err != nil {
		panic(err)
	}

	return nil
}
