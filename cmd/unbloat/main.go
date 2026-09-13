package main

import (
	"fmt"
	"os"
	"runtime"
)

// version is set at release time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println(version)
		return
	}
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "unbloat runs on Windows. It has to control WSL and Docker Desktop from outside them.")
		os.Exit(1)
	}
	fmt.Println("unbloat", version)
}
