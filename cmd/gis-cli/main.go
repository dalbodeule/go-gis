package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

type spatialOptions struct {
	operation  string
	input      string
	layer      string
	rightInput string
	rightLayer string
	output     string
	distance   float64
	profile    string
}

type mergeOptions struct {
	inputs  []string
	layers  []string
	output  string
	profile string
}

type filterOptions struct {
	input   string
	layer   string
	field   string
	value   string
	output  string
	profile string
}

type labelOptions struct {
	input         string
	layer         string
	field         string
	rotationField string
	output        string
	height        float64
	style         string
	profile       string
}

type scriptOptions struct {
	inputs      []string
	layers      []string
	script      string
	output      string
	outputLayer string
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
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
		fmt.Fprintln(os.Stdout, "usage: gis-cli [--help|--version|convert|spatial|merge|filter|label|script]")
		fmt.Fprintln(os.Stdout, "       gis-cli convert --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli spatial --operation OP --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli merge --input SOURCE --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli filter --input SOURCE --field FIELD --value VALUE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli label --input SOURCE --field FIELD --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli script --input SOURCE --script FILE.lua [--output RESULT.gpkg] [--output-layer NAME]")
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
	case "spatial":
		options, err := parseSpatialOptions(flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runSpatial(context.Background(), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "merge":
		options, err := parseMergeOptions(flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runMerge(context.Background(), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "filter":
		options, err := parseFilterOptions(flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runFilter(context.Background(), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "label":
		options, err := parseLabelOptions(flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runLabel(context.Background(), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "script":
		options, err := parseScriptOptions(flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runScript(context.Background(), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", flag.Arg(0))
		os.Exit(2)
	}
}

func parseScriptOptions(args []string) (scriptOptions, error) {
	flags := flag.NewFlagSet("script", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := scriptOptions{}
	var inputs, layers stringListFlag
	flags.Var(&inputs, "input", "input SHP or GeoPackage path; repeat for each source")
	flags.Var(&layers, "layer", "layer name; repeat once per input, or omit to load all layers")
	flags.StringVar(&options.script, "script", "", "Lua script file")
	flags.StringVar(&options.output, "output", "", "optional output GeoPackage or single-layer Shapefile")
	flags.StringVar(&options.outputLayer, "output-layer", "", "write only this result layer; required for Shapefile output")
	if err := flags.Parse(args); err != nil {
		return scriptOptions{}, err
	}
	if len(inputs) == 0 || strings.TrimSpace(options.script) == "" {
		return scriptOptions{}, fmt.Errorf("script requires at least one --input and --script")
	}
	if len(layers) != 0 && len(layers) != len(inputs) {
		return scriptOptions{}, fmt.Errorf("--layer must be provided once per --input, or omitted to load all layers")
	}
	if strings.EqualFold(filepath.Ext(options.output), ".shp") && strings.TrimSpace(options.outputLayer) == "" {
		return scriptOptions{}, fmt.Errorf("Shapefile output requires --output-layer")
	}
	if flags.NArg() != 0 {
		return scriptOptions{}, fmt.Errorf("unexpected script arguments: %v", flags.Args())
	}
	options.inputs = append([]string(nil), inputs...)
	options.layers = append([]string(nil), layers...)
	return options, nil
}

func parseSpatialOptions(args []string) (spatialOptions, error) {
	flags := flag.NewFlagSet("spatial", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := spatialOptions{}
	flags.StringVar(&options.operation, "operation", "", "spatial operation: intersect, union, difference, or buffer")
	flags.StringVar(&options.input, "input", "", "left input SHP or GeoPackage path")
	flags.StringVar(&options.layer, "layer", "", "left input layer name")
	flags.StringVar(&options.rightInput, "right-input", "", "right input SHP or GeoPackage path")
	flags.StringVar(&options.rightLayer, "right-layer", "", "right input layer name")
	flags.StringVar(&options.output, "output", "", "output DXF, GeoPackage, or SHP path")
	flags.Float64Var(&options.distance, "distance", 0, "buffer distance")
	flags.StringVar(&options.profile, "profile", "ares-utf8", "DXF profile: ares-utf8 or ares-cp949")
	if err := flags.Parse(args); err != nil {
		return spatialOptions{}, err
	}
	if options.operation == "" || options.input == "" || options.output == "" {
		return spatialOptions{}, fmt.Errorf("spatial requires --operation, --input, and --output")
	}
	switch options.operation {
	case "intersect", "union", "difference":
		if options.rightInput == "" {
			return spatialOptions{}, fmt.Errorf("%s requires --right-input", options.operation)
		}
	case "buffer":
		if options.distance == 0 {
			return spatialOptions{}, fmt.Errorf("buffer requires a non-zero --distance")
		}
	default:
		return spatialOptions{}, fmt.Errorf("unsupported spatial operation %q", options.operation)
	}
	if flags.NArg() != 0 {
		return spatialOptions{}, fmt.Errorf("unexpected spatial arguments: %v", flags.Args())
	}
	return options, nil
}

func parseMergeOptions(args []string) (mergeOptions, error) {
	flags := flag.NewFlagSet("merge", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := mergeOptions{}
	var inputs, layers stringListFlag
	flags.Var(&inputs, "input", "input SHP or GeoPackage path; repeat for each layer")
	flags.Var(&layers, "layer", "input layer name; repeat in input order")
	flags.StringVar(&options.output, "output", "", "output DXF, GeoPackage, or SHP path")
	flags.StringVar(&options.profile, "profile", "ares-utf8", "DXF profile: ares-utf8 or ares-cp949")
	if err := flags.Parse(args); err != nil {
		return mergeOptions{}, err
	}
	if len(inputs) < 2 || options.output == "" {
		return mergeOptions{}, fmt.Errorf("merge requires at least two --input values and --output")
	}
	if len(layers) != 0 && len(layers) != len(inputs) {
		return mergeOptions{}, fmt.Errorf("--layer must be provided once per --input")
	}
	if len(layers) == 0 {
		layers = make(stringListFlag, len(inputs))
	}
	if flags.NArg() != 0 {
		return mergeOptions{}, fmt.Errorf("unexpected merge arguments: %v", flags.Args())
	}
	options.inputs = append([]string(nil), inputs...)
	options.layers = append([]string(nil), layers...)
	return options, nil
}

func parseFilterOptions(args []string) (filterOptions, error) {
	flags := flag.NewFlagSet("filter", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := filterOptions{}
	flags.StringVar(&options.input, "input", "", "input SHP or GeoPackage path")
	flags.StringVar(&options.layer, "layer", "", "input layer name")
	flags.StringVar(&options.field, "field", "", "property field to match")
	flags.StringVar(&options.value, "value", "", "property value to match")
	flags.StringVar(&options.output, "output", "", "output DXF, GeoPackage, or SHP path")
	flags.StringVar(&options.profile, "profile", "ares-utf8", "DXF profile: ares-utf8 or ares-cp949")
	if err := flags.Parse(args); err != nil {
		return filterOptions{}, err
	}
	if options.input == "" || options.field == "" || options.output == "" {
		return filterOptions{}, fmt.Errorf("filter requires --input, --field, and --output")
	}
	if flags.NArg() != 0 {
		return filterOptions{}, fmt.Errorf("unexpected filter arguments: %v", flags.Args())
	}
	return options, nil
}

func parseLabelOptions(args []string) (labelOptions, error) {
	flags := flag.NewFlagSet("label", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := labelOptions{height: 1}
	flags.StringVar(&options.input, "input", "", "input SHP or GeoPackage path")
	flags.StringVar(&options.layer, "layer", "", "input layer name")
	flags.StringVar(&options.field, "field", "", "property field to use as label text")
	flags.StringVar(&options.rotationField, "rotation-field", "", "optional numeric property field for label rotation in degrees")
	flags.StringVar(&options.output, "output", "", "output DXF, GeoPackage, or SHP path")
	flags.Float64Var(&options.height, "height", 1, "label text height")
	flags.StringVar(&options.style, "style", "", "DXF text style")
	flags.StringVar(&options.profile, "profile", "ares-utf8", "DXF profile: ares-utf8 or ares-cp949")
	if err := flags.Parse(args); err != nil {
		return labelOptions{}, err
	}
	if options.input == "" || options.field == "" || options.output == "" {
		return labelOptions{}, fmt.Errorf("label requires --input, --field, and --output")
	}
	if options.height <= 0 {
		return labelOptions{}, fmt.Errorf("label --height must be positive")
	}
	if flags.NArg() != 0 {
		return labelOptions{}, fmt.Errorf("unexpected label arguments: %v", flags.Args())
	}
	return options, nil
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
