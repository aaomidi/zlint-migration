package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/dave/dst"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
	"github.com/davecgh/go-spew/spew"
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
	//file, err := parser.ParseFile(fset, filePath, oldSrc, parser.ParseComments)
	//if err != nil {
	//	return fmt.Errorf("unable to parse file: %w", err)
	//}

	file, err := decorator.ParseFile(fset, filePath, oldSrc, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("unable to parse file: %w", err)
	}

	var astutilErr error
	dstutil.Apply(file, nil, func(cursor *dstutil.Cursor) bool {

		node, ok := cursor.Node().(*dst.CallExpr)
		if !ok {
			// Not at init yet
			return true
		}
		if _, ok := node.Fun.(*dst.SelectorExpr); !ok {
			return false
		}
		// We don't want to touch non RegisterLint methods.
		if node.Fun.(*dst.SelectorExpr).Sel.Name != "RegisterLint" {
			return true
		}

		functionCall := &dst.SelectorExpr{
			X:   dst.NewIdent("lint"),
			Sel: dst.NewIdent("RegisterCertificateLint"),
		}

		metadataFields := &dst.CompositeLit{
			Type: &dst.SelectorExpr{
				X:   dst.NewIdent("lint"),
				Sel: dst.NewIdent("LintMetadata"),
			},
			Elts: nil,
		}

		certificateLintField := []dst.Expr{
			&dst.KeyValueExpr{
				Key:   dst.NewIdent("LintMetadata"),
				Value: metadataFields,
			},
			&dst.KeyValueExpr{
				Key:   dst.NewIdent("Lint"),
				Value: nil,
			},
		}

		certificateLintField[0].Decorations().Before = dst.NewLine
		certificateLintField[1].Decorations().Before = dst.NewLine

		lintCertificateLint := &dst.CompositeLit{
			Type: &dst.SelectorExpr{
				X:   dst.NewIdent("lint"),
				Sel: dst.NewIdent("CertificateLint"),
			},
			Elts: certificateLintField,
		}

		arguments := &dst.UnaryExpr{
			Op: token.AND,
			X:  lintCertificateLint,
		}

		// Loop over existing metadata fields
		for _, elt := range node.Args[0].(*dst.UnaryExpr).X.(*dst.CompositeLit).Elts {
			kv, ok := elt.(*dst.KeyValueExpr)
			if !ok {
				astutilErr = fmt.Errorf("CompositeLit field was was not KV: %v", spew.Sdump(elt))
				return false
			}
			key, ok := kv.Key.(*dst.Ident)
			if !ok {
				astutilErr = fmt.Errorf("KV key was not Ident: %v", spew.Sdump(kv.Key))
				return false
			}
			switch key.Name {
			case "Lint":
				certificateLintField[1].(*dst.KeyValueExpr).Value = kv.Value
			default:
				// TODO: Remove the Location values from these elements, maybe?
				metadataFields.Elts = append(metadataFields.Elts, kv)
			}
		}

		newCallExpr := &dst.CallExpr{
			Fun:  functionCall,
			Args: []dst.Expr{arguments},
		}
		cursor.Replace(newCallExpr)
		return true
	})

	if astutilErr != nil {
		return fmt.Errorf("issue manipulating AST: %w", astutilErr)
	}

	var buf bytes.Buffer

	if err := decorator.Fprint(&buf, file); err != nil {
		return fmt.Errorf("error when printing AST: %w", err)
	}

	// Note: since the file already exists, the permissions won't actually be modified.
	if err := os.WriteFile(filePath, buf.Bytes(), 0777); err != nil {
		return fmt.Errorf("error when writing file: %w", err)
	}

	return nil
}
