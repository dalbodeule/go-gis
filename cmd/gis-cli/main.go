package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-dev"

type convertOptions struct {
	input     string
	output    string
	layer     string
	sourceCRS string
	targetCRS string
	profile   string
}

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
		fmt.Fprintln(os.Stdout, "usage: gis-cli [--help|--version|convert]")
		fmt.Fprintln(os.Stdout, "       gis-cli convert --input SOURCE --output FILE [options]")
		return
	}

	switch flag.Arg(0) {
	case "convert":
		options, err := parseConvertOptions(flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runConvert(context.Background(), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", flag.Arg(0))
		os.Exit(2)
	}
}

func parseConvertOptions(args []string) (convertOptions, error) {
	flags := flag.NewFlagSet("convert", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := convertOptions{}
	flags.StringVar(&options.input, "input", "", "input SHP or GeoPackage path")
	flags.StringVar(&options.output, "output", "", "output DXF path")
	flags.StringVar(&options.layer, "layer", "", "input layer name")
	flags.StringVar(&options.sourceCRS, "source-crs", "", "override source CRS, for example EPSG:4326")
	flags.StringVar(&options.targetCRS, "target-crs", "", "target CRS, for example EPSG:5179")
	flags.StringVar(&options.profile, "profile", "ares-utf8", "DXF profile: ares-utf8 or ares-cp949")
	if err := flags.Parse(args); err != nil {
		return convertOptions{}, err
	}
	if options.input == "" || options.output == "" {
		return convertOptions{}, fmt.Errorf("convert requires --input and --output")
	}
	if flags.NArg() != 0 {
		return convertOptions{}, fmt.Errorf("unexpected convert arguments: %v", flags.Args())
	}
	return options, nil
}
