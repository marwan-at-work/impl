package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"marwan.io/impl"
)

const usage = `impl generates interface method stubs for a defined type
Usage:
	impl -iface=path.to/my/pkg.MyInterface -impl=path.to/my/pkg.MyTime
`

var (
	ifaceArg = flag.String("iface", "", "path to the interface declaration: path.to/my/pkg.MyInterface")
	implArg  = flag.String("impl", "", "path to the implementation type: path.to/my/pkg.MyTime")
	write    = flag.Bool("w", false, "rewrite the file instead of printing to stdout")
	wantJSON = flag.Bool("json", false, "print response infromation in json format")
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
	if len(impl.FileContent) == 0 {
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
