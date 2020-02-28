package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	"marwan.io/impl"
)

const usage = `impl generates interface method stubs for a defined type
Usage:
	impl -iface=path.to/my/pkg.MyInterface -impl=path.to/my/pkg.MyTime
	impl list # lists all available interfaces to implement
	impl list -path=io.Writer # list all available interfaces within io.Writer and its dependencies
`

var (
	ifaceArg = flag.String("iface", "", "path to the interface declaration: path.to/my/pkg.MyInterface")
	implArg  = flag.String("impl", "", "path to the implementation type: path.to/my/pkg.MyTime")
	write    = flag.Bool("w", false, "rewrite the file instead of printing to stdout")
	wantJSON = flag.Bool("json", false, "print response infromation in json format")
	path     = flag.String("path", "", "the path where you want to list interfaces (i.e. impl list -path=io.Writer)")
)

func main() {
	flag.Usage = func() {
		fmt.Println(usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return list()
		default:
			return fmt.Errorf("unrecognized command: %v", args[0])
		}
	}
	return implement()
}

func list() error {
	path, err := getPath()
	if err != nil {
		return err
	}
	ifaces, err := impl.ListInterfaces(path)
	if err != nil {
		return err
	}
	if *wantJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "\t")
		enc.Encode(ifaces)
		return nil
	}
	fmt.Printf("%s\n", strings.Join(ifaces, "\n"))
	return nil
}

func implement() error {
	ifaceArg := *ifaceArg
	implArg := *implArg
	idx := strings.LastIndex(ifaceArg, ".")
	ifacePath := ifaceArg[:idx]
	iface := ifaceArg[idx+1:]
	idx = strings.LastIndex(implArg, ".")
	implPath := implArg[:idx]
	implName := implArg[idx+1:]
	impl, err := impl.Implement(ifacePath, iface, implPath, implName)
	if err != nil {
		return err
	}
	if impl == nil || len(impl.FileContent) == 0 {
		return nil
	}
	if *write {
		return ioutil.WriteFile(impl.File, impl.FileContent, 0660)
	}
	if *wantJSON {
		bts, _ := json.MarshalIndent(impl, "", "\t")
		fmt.Printf("%s\n", bts)
		return nil
	}
	fmt.Printf("%s", impl.FileContent)
	return nil
}

func getPath() (string, error) {
	if *path != "" {
		return *path, nil
	}
	bts, err := exec.Command("go", "list", "-json").Output()
	if err != nil {
		return "", fmt.Errorf("go list err: %v", err)
	}
	var resp struct {
		ImportPath string
		Module     struct {
			Path string
		}
	}
	err = json.Unmarshal(bts, &resp)
	if err != nil {
		return "", fmt.Errorf("json decode err: %v", err)
	}
	path := resp.Module.Path
	if path == "" {
		path = resp.ImportPath
	}
	return path + "/...", nil
}
