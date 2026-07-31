package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

func main() {
	alias := flag.String("alias", "", "import alias for the production package")
	flag.Parse()
	if *alias == "" || flag.NArg() == 0 {
		panic("usage: externalize-tests -alias name files...")
	}

	dir, err := filepath.Abs(filepath.Dir(flag.Arg(0)))
	if err != nil {
		panic(err)
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   dir,
		Tests: true,
	}
	loaded, err := packages.Load(cfg, ".")
	if err != nil {
		panic(err)
	}
	if packages.PrintErrors(loaded) != 0 {
		panic("package load failed")
	}
	targets := make(map[string]bool, flag.NArg())
	for _, name := range flag.Args() {
		path, err := filepath.Abs(name)
		if err != nil {
			panic(err)
		}
		targets[path] = true
	}
	var pkg *packages.Package
	for _, candidate := range loaded {
		containsAll := true
		files := make(map[string]bool, len(candidate.CompiledGoFiles))
		for _, name := range candidate.CompiledGoFiles {
			absolute, err := filepath.Abs(name)
			if err != nil {
				panic(err)
			}
			files[absolute] = true
		}
		for target := range targets {
			if !files[target] {
				containsAll = false
				break
			}
		}
		if containsAll {
			pkg = candidate
			break
		}
	}
	if pkg == nil {
		panic("no loaded package contains every target")
	}

	for index, file := range pkg.Syntax {
		path := pkg.Fset.Position(file.Pos()).Filename
		absolute, err := filepath.Abs(path)
		if err != nil {
			panic(err)
		}
		if !targets[absolute] {
			continue
		}

		astutil.Apply(file, func(cursor *astutil.Cursor) bool {
			identifier, ok := cursor.Node().(*ast.Ident)
			if !ok {
				return true
			}
			object := pkg.TypesInfo.Uses[identifier]
			if object == nil || object.Pkg() != pkg.Types || object.Parent() != pkg.Types.Scope() || !object.Exported() {
				return true
			}
			if selector, ok := cursor.Parent().(*ast.SelectorExpr); ok && selector.Sel == identifier {
				return true
			}
			cursor.Replace(&ast.SelectorExpr{X: ast.NewIdent(*alias), Sel: ast.NewIdent(identifier.Name)})
			return false
		}, nil)

		file.Name.Name = pkg.Name + "_test"
		astutil.AddNamedImport(pkg.Fset, file, *alias, pkg.PkgPath)
		output, err := os.Create(pkg.CompiledGoFiles[index])
		if err != nil {
			panic(err)
		}
		if err := format.Node(output, pkg.Fset, file); err != nil {
			output.Close()
			panic(err)
		}
		if err := output.Close(); err != nil {
			panic(err)
		}
		fmt.Println(strconv.Quote(pkg.CompiledGoFiles[index]))
	}
}
