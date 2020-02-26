package impl

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"strings"
	"text/template"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// Implementation defines the results of
// the implement method
type Implementation struct {
	File        string   // path to the Go file of the implementing type
	FileContent []byte   // the Go file plus the method implementations at the bottom of the file
	Methods     []byte   // only the method implementations, helpful if you want to insert the methods elsewhere in the file
	Imports     []string // all the required imports for the methods, it does not filter out imports already imported by the file
	Error       error    // any error encountered during the process
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
		pkg: implPkg.Types,
		tms: types.NewMethodSet(implObj.Type()),
		pms: types.NewMethodSet(types.NewPointer(implObj.Type())),
	}
	missing, err := missingMethods(ct, ifaceObj, ifacePkg, map[string]struct{}{})
	if err != nil {
		return nil, err
	}
	if len(missing) == 0 {
		return nil, nil
	}
	allImports := []string{}
	var methodsBuffer bytes.Buffer
	for _, mm := range missing {
		imports := getInterfaceImports(mm.missing)
		allImports = append(allImports, imports...)
		addPathsToImplFile(implPkg.Fset, implFileAST, implPath, imports)
		t := template.Must(template.New("").Parse(tmpl))
		for _, m := range mm.missing {
			var sig bytes.Buffer

			nn, _ := astutil.PathEnclosingInterval(mm.file, m.Pos(), m.Pos())
			var n ast.Node = nn[1].(*ast.Field).Type
			n = astutil.Apply(n, func(c *astutil.Cursor) bool {
				sel, ok := c.Node().(*ast.SelectorExpr)
				if ok {
					return applySelector(c, sel, ifacePkg, implPath)
				}
				ident, ok := c.Node().(*ast.Ident)
				if ok {
					return applyIdentifier(c, ident, ifacePkg, implPath)
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
	return &Implementation{
		File:        implFilename,
		FileContent: source,
		Methods:     methodsBuffer.Bytes(),
		Error:       err,
		Imports:     unique(allImports),
	}, err
}

func applySelector(c *astutil.Cursor, sel *ast.SelectorExpr, ifacePkg *packages.Package, implPath string) bool {
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	obj := ifacePkg.TypesInfo.Uses[x]
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return true
	}
	if pkgName.Imported().Path() == implPath {
		c.Replace(sel.Sel)
		return false
	}
	return true
}

func applyIdentifier(c *astutil.Cursor, ident *ast.Ident, ifacePkg *packages.Package, implPath string) bool {
	obj := ifacePkg.TypesInfo.Uses[ident]
	if obj == nil {
		return true
	}
	nn := getNamed(obj.Type())
	if len(nn) == 0 {
		return true
	}
	n := nn[0]
	pkg := n.Obj().Pkg()
	if pkg == nil {
		return true
	}
	if pkg.Path() == ifacePkg.Types.Path() && pkg.Path() != implPath {
		c.Replace(&ast.SelectorExpr{
			X:   &ast.Ident{Name: obj.Pkg().Name()},
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

func addPathsToImplFile(fset *token.FileSet, f *ast.File, path string, paths []string) {
	for _, p := range paths {
		if p == path {
			continue
		}
		astutil.AddImport(fset, f, p)
	}
}

// getInterfaceImports returns all the imports
// that are required within a set of function signatures
func getInterfaceImports(funcs []*types.Func) []string {
	imps := []string{}
	for _, f := range funcs {
		imps = append(imps, getSignatureImports(f.Type().(*types.Signature))...)
	}
	return unique(imps)
}

func getSignatureImports(sig *types.Signature) []string {
	imps := []string{}
	imps = append(imps, getParamsImports(sig.Params())...)
	imps = append(imps, getParamsImports(sig.Results())...)
	return imps
}

func getParamsImports(t *types.Tuple) []string {
	imps := []string{}
	for i := 0; i < t.Len(); i++ {
		imps = append(imps, getParamImports(t.At(i))...)
	}
	return imps
}

func getParamImports(v *types.Var) []string {
	imps := []string{}
	nn := getNamed(v.Type())
	for _, named := range nn {
		pkg := named.Obj().Pkg()
		if pkg != nil {
			imps = append(imps, pkg.Path())
		}
	}
	return imps
}

func getNamed(t types.Type) []*types.Named {
	switch t := t.(type) {
	case *types.Named:
		return []*types.Named{t}
	case *types.Slice:
		return getNamed(t.Elem())
	case *types.Array:
		return getNamed(t.Elem())
	case *types.Chan:
		return getNamed(t.Elem())
	case *types.Map:
		nn := []*types.Named{}
		nn = append(nn, getNamed(t.Key())...)
		nn = append(nn, getNamed(t.Elem())...)
		return nn
	case *types.Pointer:
		return getNamed(t.Elem())
	}
	return nil
}

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
	pkg      *types.Package
	tms, pms *types.MethodSet
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

func unique(ss []string) []string {
	res := make([]string, 0, len(ss))
	mp := map[string]struct{}{}
	for _, s := range ss {
		if _, ok := mp[s]; !ok {
			mp[s] = struct{}{}
			res = append(res, s)
		}
	}
	return res
}
