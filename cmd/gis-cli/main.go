package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	help := flag.Bool("help", false, "show usage information")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *help || flag.NArg() == 0 {
		fmt.Fprintln(os.Stdout, "gogis CLI")
		fmt.Fprintln(os.Stdout, "usage: gis-cli [--help|--version]")
		return
	}

	fmt.Fprintf(os.Stderr, "command %q is not implemented yet\n", flag.Arg(0))
	os.Exit(2)
}
