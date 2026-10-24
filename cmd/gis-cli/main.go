package main

import (
	"context"
	"errors"
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
	input    string
	layer    string
	field    string
	value    string
	output   string
	profile  string
	fieldMap string
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

type workflowOptions struct {
	input, table, output, layer, outputLayer, xField, yField, crs, encoding, delimiter, layerKey, tableKey, required, unique, fieldMap, featureIDs, field, template, geometryType, groupField, sumField, numberFields, boolFields string
	allowInvalid                                                                                                                                                                                                                  bool
	inputs                                                                                                                                                                                                                        []string
	buildBoundary                                                                                                                                                                                                                 bool
	boundaryGroup                                                                                                                                                                                                                 string
	boundaryFile, boundaryLayer                                                                                                                                                                                                   string
}

type postGISOptions struct {
	input   string
	output  string
	dsn     string
	table   string
	layer   string
	profile string
	id      uint64
	idSet   bool
	wkt     string
	crs     string
	props   string
	schema  string
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
		fmt.Fprintln(os.Stdout, "usage: gis-cli [--help|--version|convert|spatial|merge|filter|label|import-csv|split-boundary|join|validate|measure|script|postgis-list|postgis-import|postgis-export|postgis-insert|postgis-delete]")
		fmt.Fprintln(os.Stdout, "       gis-cli convert --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli spatial --operation OP --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli merge --input SOURCE --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli filter --input SOURCE --field FIELD --value VALUE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli label --input SOURCE --field FIELD --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli create-layer --output FILE --output-layer NAME --geometry-type POINT|LINESTRING|POLYGON --crs EPSG:4326")
		fmt.Fprintln(os.Stdout, "       gis-cli import-csv --input CSV [--input CSV...] --output FILE --x-field X --y-field Y --crs EPSG:4326 [--boundary --boundary-group FIELD]")
		fmt.Fprintln(os.Stdout, "       gis-cli split-boundary --input SOURCE.shp --boundary-file BOUNDARIES.gpkg --boundary-layer NAME --output RESULT.gpkg")
		fmt.Fprintln(os.Stdout, "       gis-cli join --input SOURCE --table CSV --layer-key FIELD --table-key FIELD --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli polygon --input SOURCE --layer NAME --feature-ids 1,2,3 --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli label-field --input SOURCE --field label_text --template '시설-{관리번호}' --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli validate|measure --input SOURCE --output FILE [options]")
		fmt.Fprintln(os.Stdout, "       gis-cli aggregate --input SOURCE --group FIELD --sum FIELD --output REPORT.csv")
		fmt.Fprintln(os.Stdout, "       gis-cli script --input SOURCE --script FILE.lua [--output RESULT.gpkg] [--output-layer NAME]")
		fmt.Fprintln(os.Stdout, "       gis-cli postgis-import --input SOURCE --dsn DSN --table schema.table [--layer NAME]")
		fmt.Fprintln(os.Stdout, "       gis-cli postgis-list --dsn DSN [--schema SCHEMA]")
		fmt.Fprintln(os.Stdout, "       gis-cli postgis-export --dsn DSN --table schema.table --output FILE [--profile PROFILE]")
		fmt.Fprintln(os.Stdout, "       gis-cli postgis-insert --dsn DSN --table schema.table --id ID --wkt WKT --crs EPSG:4326 [--properties JSON]")
		fmt.Fprintln(os.Stdout, "       gis-cli postgis-delete --dsn DSN --table schema.table --id ID")
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
	case "create-layer", "import-csv", "split-boundary", "join", "validate", "measure", "polygon", "label-field", "aggregate":
		options, err := parseWorkflowOptions(flag.Arg(0), flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runWorkflow(context.Background(), flag.Arg(0), options); err != nil {
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
	case "postgis-list", "postgis-import", "postgis-export", "postgis-insert", "postgis-delete":
		options, err := parsePostGISOptions(flag.Arg(0), flag.Args()[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runPostGIS(context.Background(), flag.Arg(0), options); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", flag.Arg(0))
		os.Exit(2)
	}
}

func parsePostGISOptions(command string, args []string) (postGISOptions, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := postGISOptions{}
	flags.StringVar(&options.input, "input", "", "local input SHP or GeoPackage path")
	flags.StringVar(&options.output, "output", "", "local output DXF, GeoPackage, or Shapefile path")
	flags.StringVar(&options.dsn, "dsn", "", "PostgreSQL connection string")
	flags.StringVar(&options.table, "table", "", "PostGIS table, optionally schema-qualified")
	flags.StringVar(&options.layer, "layer", "", "input layer name")
	flags.StringVar(&options.profile, "profile", "ares-utf8", "DXF profile: ares-utf8 or ares-cp949")
	flags.Uint64Var(&options.id, "id", 0, "feature ID")
	flags.StringVar(&options.wkt, "wkt", "", "feature geometry as WKT")
	flags.StringVar(&options.crs, "crs", "", "feature CRS, for example EPSG:4326")
	flags.StringVar(&options.props, "properties", "{}", "feature properties as JSON object")
	flags.StringVar(&options.schema, "schema", "", "optional PostgreSQL schema filter")
	if err := flags.Parse(args); err != nil {
		return postGISOptions{}, err
	}
	for _, arg := range args {
		if arg == "--id" || strings.HasPrefix(arg, "--id=") {
			options.idSet = true
			break
		}
	}
	if options.dsn == "" || options.table == "" {
		if command != "postgis-list" || options.dsn == "" {
			return postGISOptions{}, fmt.Errorf("%s requires --dsn and --table", command)
		}
	}
	switch command {
	case "postgis-list":
		// The DSN is the only required value for a catalog request.
	case "postgis-import":
		if options.input == "" {
			return postGISOptions{}, fmt.Errorf("postgis-import requires --input")
		}
	case "postgis-export":
		if options.output == "" {
			return postGISOptions{}, fmt.Errorf("postgis-export requires --output")
		}
	case "postgis-insert":
		if options.wkt == "" || options.crs == "" || !options.idSet {
			return postGISOptions{}, fmt.Errorf("postgis-insert requires --id, --wkt, and --crs")
		}
	case "postgis-delete":
		if !options.idSet {
			return postGISOptions{}, fmt.Errorf("postgis-delete requires --id")
		}
	default:
		return postGISOptions{}, fmt.Errorf("unsupported PostGIS command %q", command)
	}
	if flags.NArg() != 0 {
		return postGISOptions{}, fmt.Errorf("unexpected %s arguments: %v", command, flags.Args())
	}
	return options, nil
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
	flags.StringVar(&options.fieldMap, "field-map", "", "output field mapping old=new[,old=new]")
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

func parseWorkflowOptions(command string, args []string) (workflowOptions, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var options workflowOptions
	var inputs stringListFlag
	flags.Var(&inputs, "input", "input source or CSV path (repeat for CSV merge)")
	flags.StringVar(&options.table, "table", "", " 업무대장 CSV path")
	flags.StringVar(&options.output, "output", "", "output GeoPackage or Shapefile path")
	flags.StringVar(&options.layer, "layer", "", "input layer name")
	flags.StringVar(&options.outputLayer, "output-layer", "", "output layer name")
	flags.StringVar(&options.xField, "x-field", "", "X coordinate field")
	flags.StringVar(&options.yField, "y-field", "", "Y coordinate field")
	flags.StringVar(&options.crs, "crs", "", "source CRS, for example EPSG:4326")
	flags.StringVar(&options.encoding, "encoding", "UTF-8", "UTF-8 or CP949")
	flags.StringVar(&options.delimiter, "delimiter", ",", "CSV delimiter, for example ',' or tab")
	flags.StringVar(&options.layerKey, "layer-key", "", "layer join key")
	flags.StringVar(&options.tableKey, "table-key", "", "table join key")
	flags.StringVar(&options.required, "required", "", "comma-separated required fields")
	flags.StringVar(&options.unique, "unique", "", "unique key field")
	flags.StringVar(&options.fieldMap, "field-map", "", "output field mapping old=new[,old=new]")
	flags.StringVar(&options.featureIDs, "feature-ids", "", "ordered feature IDs, comma-separated")
	flags.StringVar(&options.field, "field", "", "label field name")
	flags.StringVar(&options.template, "template", "", "label template, for example 시설-{관리번호}")
	flags.StringVar(&options.geometryType, "geometry-type", "", "empty layer geometry type")
	flags.StringVar(&options.groupField, "group", "", "aggregation group field")
	flags.StringVar(&options.sumField, "sum", "", "aggregation numeric sum field")
	flags.StringVar(&options.numberFields, "number-fields", "", "comma-separated numeric CSV fields")
	flags.StringVar(&options.boolFields, "bool-fields", "", "comma-separated boolean CSV fields")
	flags.BoolVar(&options.allowInvalid, "allow-invalid", false, "import valid rows while reporting rejected rows")
	flags.BoolVar(&options.buildBoundary, "boundary", false, "calculate convex boundaries and shape IDs from CSV points")
	flags.StringVar(&options.boundaryGroup, "boundary-group", "", "optional CSV field that identifies independent boundary groups")
	flags.StringVar(&options.boundaryFile, "boundary-file", "", "GeoPackage containing identified boundary polygons")
	flags.StringVar(&options.boundaryLayer, "boundary-layer", "", "boundary layer name")
	if err := flags.Parse(args); err != nil {
		return workflowOptions{}, err
	}
	options.inputs = append([]string(nil), inputs...)
	if len(inputs) > 0 {
		options.input = inputs[0]
	}
	if len(inputs) > 1 && command != "import-csv" {
		return workflowOptions{}, fmt.Errorf("repeated --input is only supported for import-csv")
	}
	if flags.NArg() != 0 {
		return workflowOptions{}, fmt.Errorf("unexpected %s arguments: %v", command, flags.Args())
	}
	switch command {
	case "import-csv":
		if options.input == "" || options.output == "" || options.xField == "" || options.yField == "" || options.crs == "" {
			return workflowOptions{}, errors.New("import-csv requires --input, --output, --x-field, --y-field, and --crs")
		}
		if options.buildBoundary && !strings.EqualFold(filepath.Ext(options.output), ".gpkg") {
			return workflowOptions{}, errors.New("--boundary requires GeoPackage output for point and boundary layers")
		}
	case "split-boundary":
		if options.input == "" || options.boundaryFile == "" || options.boundaryLayer == "" || options.output == "" || !strings.EqualFold(filepath.Ext(options.output), ".gpkg") {
			return workflowOptions{}, errors.New("split-boundary requires --input, --boundary-file, --boundary-layer, and --output RESULT.gpkg")
		}
	case "create-layer":
		if options.output == "" || options.outputLayer == "" || options.geometryType == "" || options.crs == "" {
			return workflowOptions{}, errors.New("create-layer requires --output, --output-layer, --geometry-type, and --crs")
		}
	case "join":
		if options.input == "" || options.table == "" || options.output == "" || options.layerKey == "" || options.tableKey == "" {
			return workflowOptions{}, errors.New("join requires --input, --table, --output, --layer-key, and --table-key")
		}
	case "polygon":
		if options.input == "" || options.output == "" || options.featureIDs == "" {
			return workflowOptions{}, errors.New("polygon requires --input, --feature-ids, and --output")
		}
	case "label-field":
		if options.input == "" || options.output == "" || options.field == "" {
			return workflowOptions{}, errors.New("label-field requires --input, --field, and --output")
		}
	case "aggregate":
		if options.input == "" || options.output == "" || options.groupField == "" {
			return workflowOptions{}, errors.New("aggregate requires --input, --group, and --output")
		}
	default:
		if options.input == "" || options.output == "" {
			return workflowOptions{}, fmt.Errorf("%s requires --input and --output", command)
		}
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
