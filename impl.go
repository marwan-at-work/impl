package impl

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"text/template"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// Implementation defines the results of
// the implement method
type Implementation struct {
	File         string         // path to the Go file of the implementing type
	FileContent  []byte         // the Go file plus the method implementations at the bottom of the file
	Methods      []byte         // only the method implementations, helpful if you want to insert the methods elsewhere in the file
	AddedImports []*AddedImport // all the required imports for the methods, it does not filter out imports already imported by the file
	AllImports   []*AddedImport // convenience to get a list of all the imports of the concrete type file
	Error        error          // any error encountered during the process
}

// AddedImport represents a newly added import
// statement to the concrete type. If name is not
// empty, then that import is required to have that name.
type AddedImport struct {
	Name, Path string
}

// Implement an interface and return the path to as well as the content of the
// file where the concrete type was defined updated with all of the missing methods
func Implement(ifacePath, iface, implPath, impl string) (*Implementation, error) {
	ifacePkg, implPkg, err := loadPackages(ifacePath, implPath)
	if err != nil {
		return nil, err
	}
	ifaceObj := ifacePkg.Types.Scope().Lookup(iface)
	if ifaceObj == nil {
		return nil, fmt.Errorf("could not find interface declaration (%s) in %s", iface, ifacePath)
	}
	implObj := implPkg.Types.Scope().Lookup(impl)
	if implObj == nil {
		return nil, fmt.Errorf("could not find type declaration (%s) in %s", impl, implPath)
	}
	implFilename, implFileAST := getFile(implPkg, implObj)
	ct := &concreteType{
		pkg:  implPkg.Types,
		fset: implPkg.Fset,
		file: implFileAST,
		tms:  types.NewMethodSet(implObj.Type()),
		pms:  types.NewMethodSet(types.NewPointer(implObj.Type())),
	}
	missing, err := missingMethods(ct, ifaceObj, ifacePkg, map[string]struct{}{})
	if err != nil {
		return nil, err
	}
	if len(missing) == 0 {
		return nil, nil
	}
	var methodsBuffer bytes.Buffer
	for _, mm := range missing {
		// imports := getInterfaceImports(mm.missing)
		// allImports = append(allImports, imports...)
		// addPathsToImplFile(implPkg.Fset, implFileAST, implPath, imports)
		t := template.Must(template.New("").Parse(tmpl))
		for _, m := range mm.missing {
			var sig bytes.Buffer

			nn, _ := astutil.PathEnclosingInterval(mm.file, m.Pos(), m.Pos())
			var n ast.Node = nn[1].(*ast.Field).Type
			n = astutil.Apply(n, func(c *astutil.Cursor) bool {
				sel, ok := c.Node().(*ast.SelectorExpr)
				if ok {
					renamed := mightRenameSelector(c, sel, ifacePkg, ct)
					removed := mightRemoveSelector(c, sel, ifacePkg, implPath)
					return removed || renamed
				}
				ident, ok := c.Node().(*ast.Ident)
				if ok {
					return mightAddSelector(c, ident, ifacePkg, ct)
				}
				return true
			}, nil)
			err = format.Node(&sig, ifacePkg.Fset, n)
			if err != nil {
				return nil, fmt.Errorf("could not format function signature: %w", err)
			}
			md := methodData{
				Name:        m.Name(),
				Implementer: impl,
				Interface:   iface,
				Signature:   strings.TrimPrefix(sig.String(), "func"),
			}
			err = t.Execute(&methodsBuffer, md)
			if err != nil {
				return nil, fmt.Errorf("error executing method template: %w", err)
			}
			methodsBuffer.WriteRune('\n')
		}
	}
	var buf bytes.Buffer
	format.Node(&buf, implPkg.Fset, implFileAST)
	buf.Write(methodsBuffer.Bytes())
	source, err := format.Source(buf.Bytes())
	allImports := []*AddedImport{}
	for _, imp := range implFileAST.Imports {
		ai := &AddedImport{"", imp.Path.Value}
		if imp.Name != nil {
			ai.Name = imp.Name.Name
		}
		allImports = append(allImports, ai)
	}
	return &Implementation{
		File:         implFilename,
		FileContent:  source,
		Methods:      methodsBuffer.Bytes(),
		Error:        err,
		AddedImports: ct.addedImports,
		AllImports:   allImports,
	}, err
}

// mightRemoveSelector will replace a selector such as *models.User to just be *User.
// This is needed if the interface method imports the same package where the concrete type
// is going to implement that method
func mightRemoveSelector(c *astutil.Cursor, sel *ast.SelectorExpr, ifacePkg *packages.Package, implPath string) bool {
	obj := ifacePkg.TypesInfo.Uses[sel.Sel]
	if obj.Pkg().Path() == implPath {
		c.Replace(sel.Sel)
		return false
	}
	return true
}

// mightRenameSelector will take a selector such as *models.User and rename it to *somethingelse.User
// if the target conrete type file already imports the "models" package but has renamed it.
// If the concrete type does not have the import file, then the import file will be added along with its
// rename if the interface file has defined one.
func mightRenameSelector(c *astutil.Cursor, sel *ast.SelectorExpr, ifacePkg *packages.Package, ct *concreteType) bool {
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return true
	}
	obj := ifacePkg.TypesInfo.Uses[ident]
	if obj == nil {
		return true
	}
	pn, ok := obj.(*types.PkgName)
	if !ok {
		return true
	}
	pkg := pn.Imported()
	var hasImport bool
	var importName string
	for _, imp := range ct.file.Imports {
		impPath, _ := strconv.Unquote(imp.Path.Value)
		if impPath == pkg.Path() {
			hasImport = true
			importName = pkg.Name()
			if imp.Name != nil && imp.Name.Name != pkg.Name() {
				importName = imp.Name.Name
			}
			break
		}
	}
	if hasImport {
		ident.Name = importName
		c.Replace(sel)
		return false
	}
	if pkg.Path() == ct.pkg.Path() {
		return true
	}
	if pn.Name() != pkg.Name() {
		importName = pn.Name()
	}
	ct.addImport(importName, pkg.Path())
	return false
}

// mightAddSelector takes an identifier such as "User" and might turn into a selector
// such as "models.User". This is needed when an interface method references
// a type declaration in its own package while the concrete type is in a different package.
// If an import already exists, it will use that import's name. If it does not exist,
// it will add it to the ct's *ast.File.
func mightAddSelector(
	c *astutil.Cursor,
	ident *ast.Ident,
	ifacePkg *packages.Package,
	ct *concreteType,
) bool {
	obj := ifacePkg.TypesInfo.Uses[ident]
	if obj == nil {
		return true
	}
	n, ok := obj.Type().(*types.Named)
	if !ok {
		return true
	}
	pkg := n.Obj().Pkg()
	if pkg == nil {
		return true
	}
	if pkg.Path() == ifacePkg.Types.Path() && pkg.Path() != ct.pkg.Path() {
		pkgName := pkg.Name()
		missingImport := true
		for _, imp := range ct.file.Imports {
			impPath, _ := strconv.Unquote(imp.Path.Value)
			if pkg.Path() == impPath {
				missingImport = false
				if imp.Name != nil {
					pkgName = imp.Name.Name
				}
				break
			}
		}
		if missingImport {
			ct.addImport("", pkg.Path())
		}
		c.Replace(&ast.SelectorExpr{
			X:   &ast.Ident{Name: pkgName},
			Sel: ident,
		})
		return false
	}
	return true
}

type methodData struct {
	Name        string
	Interface   string
	Implementer string
	Signature   string
}

const tmpl = `// {{ .Name }} implements {{ .Interface }}
func (*{{ .Implementer }}) {{ .Name }}{{ .Signature }} {
	panic("unimplemented")
}
`

func loadPackages(ifacePath, implPath string) (ifacePkg *packages.Package, implPkg *packages.Package, err error) {
	var cfg packages.Config
	cfg.Mode = packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps
	pkgs, err := packages.Load(&cfg, ifacePath, implPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error loading packages: %w", err)
	}
	for _, p := range pkgs {
		pkgPath := p.Types.Path()
		if pkgPath == ifacePath {
			ifacePkg = p
		} else if pkgPath == implPath {
			implPkg = p
		}
	}
	if ifacePkg == nil {
		return nil, nil, fmt.Errorf("missing interface package info for %v", ifacePath)
	} else if implPkg == nil {
		return nil, nil, fmt.Errorf("missing implementation package info for %v", ifacePath)
	}
	return ifacePkg, implPkg, nil
}

type mismatchError struct {
	name       string
	have, want *types.Signature
}

func (me *mismatchError) Error() string {
	return fmt.Sprintf("mimsatched %q function singatures:\nhave: %s\nwant: %s", me.name, me.have, me.want)
}

// missingInterface represents an interface
// that has all or some of its methods missing
// from the destination concrete type
type missingInterface struct {
	iface   *types.Interface
	file    *ast.File
	missing []*types.Func
}

// concreteType is the destination type
// that will implement the interface methods
type concreteType struct {
	pkg          *types.Package
	fset         *token.FileSet
	file         *ast.File
	tms, pms     *types.MethodSet
	addedImports []*AddedImport
}

func (ct *concreteType) doesNotHaveMethod(name string) bool {
	return ct.tms.Lookup(ct.pkg, name) == nil && ct.pms.Lookup(ct.pkg, name) == nil
}

func (ct *concreteType) getMethodSelection(name string) *types.Selection {
	if sel := ct.tms.Lookup(ct.pkg, name); sel != nil {
		return sel
	}
	return ct.pms.Lookup(ct.pkg, name)
}

func (ct *concreteType) addImport(name, path string) {
	astutil.AddNamedImport(ct.fset, ct.file, name, path)
	ct.addedImports = append(ct.addedImports, &AddedImport{name, path})
}

/*
missingMethods takes a concrete type and returns any missing methods for the given interface as well as
any missing interface that might have been embedded to its parent. For example:

type I interface {
	io.Writer
	Hello()
}
returns []*missingInterface{
	{
		iface: *types.Interface (io.Writer),
		file: *ast.File: io.go,
		missing []*types.Func{Write},
	},
	{
		iface: *types.Interface (I),
		file: *ast.File: myfile.go,
		missing: []*types.Func{Hello}
	},
}
*/
func missingMethods(ct *concreteType, ifaceObj types.Object, ifacePkg *packages.Package, visited map[string]struct{}) ([]*missingInterface, error) {
	iface, ok := ifaceObj.Type().Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("expected %v to be an interface but got %T", iface, ifaceObj.Type().Underlying())
	}
	missing := []*missingInterface{}
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		eiface := iface.Embedded(i).Obj()
		depPkg := ifacePkg
		if eiface.Pkg().Path() != ifacePkg.Types.Path() {
			depPkg = ifacePkg.Imports[eiface.Pkg().Path()]
			if depPkg == nil {
				return nil, fmt.Errorf("missing dependency for %v", eiface.Name())
			}
		}
		em, err := missingMethods(ct, eiface, depPkg, visited)
		if err != nil {
			return nil, err
		}
		missing = append(missing, em...)
	}
	_, astFile := getFile(ifacePkg, ifaceObj)
	mm := &missingInterface{
		iface: iface,
		file:  astFile,
	}
	if mm.file == nil {
		return nil, fmt.Errorf("could not find ast.File for %v", ifaceObj.Name())
	}
	for i := 0; i < iface.NumExplicitMethods(); i++ {
		method := iface.ExplicitMethod(i)
		if ct.doesNotHaveMethod(method.Name()) {
			if _, ok := visited[method.Name()]; !ok {
				mm.missing = append(mm.missing, method)
				visited[method.Name()] = struct{}{}
			}
		}
		if sel := ct.getMethodSelection(method.Name()); sel != nil {
			implSig := sel.Type().(*types.Signature)
			ifaceSig := method.Type().(*types.Signature)
			if !equalSignatures(ifaceSig, implSig) {
				return nil, &mismatchError{
					name: method.Name(),
					have: implSig,
					want: ifaceSig,
				}
			}
		}
	}
	if len(mm.missing) > 0 {
		missing = append(missing, mm)
	}
	return missing, nil
}

func equalSignatures(sig, sig2 *types.Signature) bool {
	if sig.Variadic() != sig2.Variadic() {
		return false
	}
	if !equalParams(sig.Params(), sig2.Params()) {
		return false
	}
	if !equalParams(sig.Results(), sig2.Results()) {
		return false
	}
	return true
}

func equalResults() bool {
	return true
}

func equalParams(p, p2 *types.Tuple) bool {
	if p == nil && p2 != nil {
		return false
	}
	if p != nil && p2 == nil {
		return false
	}
	if p == nil && p2 == nil {
		return true
	}
	if p.Len() != p2.Len() {
		return false
	}
	for i := 0; i < p.Len(); i++ {
		if !equalParam(p.At(i), p2.At(i)) {
			return false
		}
	}
	return true
}

func equalParam(p, p2 *types.Var) bool {
	return p.Type().String() == p2.Type().String()
}

// getFile returns the local path to as well as the AST of a Go file where
// the given types.Object was defined.
func getFile(pkg *packages.Package, obj types.Object) (string, *ast.File) {
	file := pkg.Fset.Position(obj.Pos()).Filename
	for _, s := range pkg.Syntax {
		f := pkg.Fset.Position(s.Pos()).Filename
		if strings.HasSuffix(f, file) {
			return file, s
		}
	}
	return "", nil
}
