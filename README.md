# impl

A library and a command line that intelligently implements Go interfaces for any given type.


### Features:

- [x] Go Modules aware
- [x] Only adds the missing methods
- [x] Reports an error if a method with a conflicting type signature already exists
- [x] Adds "import" declarations to the file if any of the interface methods require it
- [x] Recursively implement methods in embedded interfaces
- [x] Adjusts the method function signature based on imports, such as replacing `*models.Person` with `*Person` if the target is in the "models" package already.
- [x] Understands "." imports as well as "_" named imports
 
### Install

As a command line tool:

`go get marwan.io/impl/cmd/impl`

As a library: 

`go get marwan.io/impl`

### Usage (command line)

Given a `type MyType struct {}` definition in github.com/my/pkg, you can run the following command:

`impl -iface=io.Writer -impl=github.com/my/pkg.MyType` 

And whichever file MyType is defined in will have `func (*MyType) Write(p []byte) (int, error) { panic("unimplemented) }` 

Similar to gofmt, results will be printed to stdout by default. If you'd like to persist the file instead, then pass the `-w` flag.

For other options such as json output for tooling, see `impl --help`.

### Usage (library)

```golang
import "marwan.io/impl"

resp, err := impl.Implement("io", "Writer", "github.com/my/pkg", "MyType")
// resp.File is the file where MyType was defined
// resp.FileContent is the new content of the entire file that has the Writer method
// resp.Methods is the Go code of all the newly add methods
// resp.AddedImports denotes all new import statements to the concrete type file
// resp.AllImports denotes all import paths of that same file.
// err must be handled
```

### List Available Interfaces

The library can list available interfaces given any import path

```bash
impl list -json
["mypkg.MyInterface", "io.Writer", "io.Closer"] # etc

impl list io -json
# ...
```
