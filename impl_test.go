package impl

import (
	"flag"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var implementTests = []struct {
	name        string
	description string
	ifacePath   string
	iface       string
	implPath    string
	impl        string
	goldenFile  string
}{
	{
		name:       "std lib interface",
		ifacePath:  "io",
		iface:      "Writer",
		implPath:   "marwan.io/impl/test_data/goer",
		impl:       "Goer",
		goldenFile: "test_data/goer/writer.golden",
	},
	{
		name: "remove self import",
		description: `
			If the interface declaration imports a type
			that happens to be in the same file we want
			to implement that interface, make sure that
			file deosn't accidentally import itself.
		`,
		ifacePath:  "marwan.io/impl/test_data/rioter",
		iface:      "Rioter",
		implPath:   "marwan.io/impl/test_data/crowd",
		impl:       "Crowd",
		goldenFile: "test_data/crowd/rioter.golden",
	},
	{
		name: "add self import",
		description: `
			If the interface defines a type within
			its own package, then we want to add the import
			as well as the selector to the method signature
			of the destination type.
		`,
		ifacePath:  "marwan.io/impl/test_data/partier",
		iface:      "Partier",
		implPath:   "marwan.io/impl/test_data/goer",
		impl:       "Goer",
		goldenFile: "test_data/goer/partier.golden",
	},
	{
		name: "underscored imports",
		description: `
			If there's an underscore in the concrete type 
			file, it must be ignored and re-imported if need be.
		`,
		ifacePath:  "marwan.io/impl/test_data/simple",
		iface:      "Interface",
		implPath:   "marwan.io/impl/test_data/underscore",
		impl:       "Underscore",
		goldenFile: "test_data/underscore/simple.golden",
	},
	{
		name: "dot imports",
		description: `
			If there's a dot import being used in the interface,
			make sure the concrete type adds it as a regular import.
		`,
		ifacePath:  "marwan.io/impl/test_data/dotter",
		iface:      "Interface",
		implPath:   "marwan.io/impl/test_data/underscore",
		impl:       "Underscore",
		goldenFile: "test_data/underscore/dotter.golden",
	},
}

var u = flag.Bool("u", false, "override and update golden files")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func TestImplement(t *testing.T) {
	for _, tc := range implementTests {
		t.Run(tc.name, func(t *testing.T) {
			imp, err := Implement(tc.ifacePath, tc.iface, tc.implPath, tc.impl)
			if err != nil {
				t.Fatal(err)
			}
			if *u {
				err := ioutil.WriteFile(tc.goldenFile, imp.FileContent, 0660)
				if err != nil {
					t.Fatalf("could not write %q golden file: %v", tc.goldenFile, err)
				}
				return
			}
			want, err := ioutil.ReadFile(tc.goldenFile)
			if err != nil {
				t.Fatal(err)
			}
			require.Equal(t, string(want), string(imp.FileContent), "expected to match golden file")
		})
	}
}

func BenchmarkImplementation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := Implement("marwan.io/impl/test_data/partier", "Partier", "marwan.io/impl/test_data/goer", "Goer")
		if err != nil {
			b.Fatal(err)
		}
	}
}
