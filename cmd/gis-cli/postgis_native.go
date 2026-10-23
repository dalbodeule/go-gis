//go:build native

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"gogis/drivers/dxf"
	"gogis/drivers/gdal"
	"gogis/drivers/postgis"
	"gogis/internal/core"
)

func runPostGIS(ctx context.Context, command string, options postGISOptions) error {
	if command == "postgis-list" {
		tables, err := postgis.ListTables(ctx, options.dsn, options.schema)
		if err != nil {
			return err
		}
		sort.Slice(tables, func(i, j int) bool {
			if tables[i].Schema != tables[j].Schema {
				return tables[i].Schema < tables[j].Schema
			}
			return tables[i].Name < tables[j].Name
		})
		for _, table := range tables {
			fmt.Printf("%s.%s\t%s\tEPSG:%d\n", table.Schema, table.Name, table.GeometryColumn, table.SRID)
		}
		return nil
	}
	store, err := postgis.Open(ctx, options.dsn, options.table)
	if err != nil {
		return fmt.Errorf("open PostGIS: %w", err)
	}
	defer store.Close()

	switch command {
	case "postgis-import":
		layer, err := (gdal.Reader{}).Open(ctx, options.input, options.layer)
		if err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		if err := store.SaveLayer(ctx, layer); err != nil {
			return fmt.Errorf("write PostGIS table %q: %w", options.table, err)
		}
		return nil
	case "postgis-export":
		layer, err := store.ReadLayer(ctx)
		if err != nil {
			return fmt.Errorf("read PostGIS table %q: %w", options.table, err)
		}
		if filepath.Ext(options.output) == ".dxf" {
			if err := (dxf.Exporter{}).Export(ctx, options.output, layer, options.profile); err != nil {
				return fmt.Errorf("write DXF: %w", err)
			}
			return nil
		}
		if err := (gdal.Writer{}).Write(ctx, options.output, layer); err != nil {
			return fmt.Errorf("write vector output: %w", err)
		}
		return nil
	case "postgis-insert":
		properties := map[string]any{}
		if err := json.Unmarshal([]byte(options.props), &properties); err != nil {
			return fmt.Errorf("parse properties: %w", err)
		}
		if err := store.AppendFeature(ctx, core.CRS{AuthorityCode: options.crs}, core.Feature{
			ID: options.id, Geometry: core.WKTGeometry{WKT: options.wkt}, Properties: properties,
		}); err != nil {
			return fmt.Errorf("insert PostGIS feature: %w", err)
		}
		return nil
	case "postgis-delete":
		if err := store.DeleteFeature(ctx, options.id); err != nil {
			return fmt.Errorf("delete PostGIS feature: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported PostGIS command %q", command)
	}
}
