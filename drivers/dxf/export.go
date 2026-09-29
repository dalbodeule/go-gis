package dxf

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gogis/internal/core"
	"golang.org/x/text/encoding/korean"
)

// Profile controls the deliberately explicit DXF header choices.
type Profile struct {
	// ACADVersion is an AutoCAD version such as AC1015.
	ACADVersion string
	// CodePage is written to $DWGCODEPAGE and must be validated against the CAD target.
	CodePage   string
	TextHeight float64
}

// ARESUTF8 is an experimental profile. ARES compatibility and Korean font
// rendering still require verification with the target application.
var ARESUTF8 = Profile{ACADVersion: "AC1015", CodePage: "UTF-8", TextHeight: 1}

// ARESCP949 is the legacy Korean code-page profile. ARES/CAD compatibility
// still needs validation in the target application, but the byte encoding and
// declared DXF header are now deterministic.
var ARESCP949 = Profile{ACADVersion: "AC1015", CodePage: "ANSI_949", TextHeight: 1}

// Exporter writes a small, inspectable ASCII DXF subset without hiding header
// or encoding decisions behind a third-party library.
type Exporter struct {
	Profile Profile
}

// Export writes Point, LineString, Polygon and a property-driven TEXT label.
func (e Exporter) Export(ctx context.Context, destination string, layer core.Layer, profile string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configuration := e.Profile
	if configuration.ACADVersion == "" {
		configuration = ARESUTF8
	}
	switch profile {
	case "", "ares-utf8":
	case "ares-cp949":
		configuration = ARESCP949
	default:
		return fmt.Errorf("unsupported DXF profile %q", profile)
	}
	if _, err := encodeText("", configuration.CodePage); err != nil {
		return err
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	write := func(code int, value string) error {
		encoded, err := encodeText(value, configuration.CodePage)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "%d\n", code); err != nil {
			return err
		}
		if _, err := writer.Write(encoded); err != nil {
			return err
		}
		return writer.WriteByte('\n')
	}
	if err := writeHeader(write, configuration); err != nil {
		return err
	}
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return err
		}
		geometry, ok := feature.Geometry.(core.WKTGeometry)
		if !ok {
			return fmt.Errorf("feature %d geometry is not core.WKTGeometry", feature.ID)
		}
		if err := writeWKT(write, geometry.WKT, layer.Name); err != nil {
			return fmt.Errorf("feature %d: %w", feature.ID, err)
		}
		if label, ok := feature.Properties["label"].(string); ok && label != "" {
			if err := writeText(write, label, configuration.TextHeight); err != nil {
				return err
			}
		}
	}
	if err := write(0, "ENDSEC"); err != nil {
		return err
	}
	if err := write(0, "EOF"); err != nil {
		return err
	}
	return writer.Flush()
}

func encodeText(value, codePage string) ([]byte, error) {
	switch strings.ToUpper(strings.TrimSpace(codePage)) {
	case "UTF-8", "UTF8":
		return []byte(value), nil
	case "CP949", "ANSI_949", "EUC-KR":
		encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(value))
		if err != nil {
			return nil, fmt.Errorf("encode DXF text as CP949: %w", err)
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("unsupported DXF code page %q", codePage)
	}
}

func writeHeader(write func(int, string) error, profile Profile) error {
	if err := write(0, "SECTION"); err != nil {
		return err
	}
	if err := write(2, "HEADER"); err != nil {
		return err
	}
	for _, item := range [][2]string{{"$ACADVER", profile.ACADVersion}, {"$DWGCODEPAGE", profile.CodePage}} {
		if err := write(9, item[0]); err != nil {
			return err
		}
		if err := write(1, item[1]); err != nil {
			return err
		}
	}
	if err := write(0, "ENDSEC"); err != nil {
		return err
	}
	if err := write(0, "SECTION"); err != nil {
		return err
	}
	return write(2, "ENTITIES")
}

func writeWKT(write func(int, string) error, wkt, layer string) error {
	upper := strings.ToUpper(strings.TrimSpace(wkt))
	if strings.HasPrefix(upper, "POINT") {
		values, err := numbers(wkt)
		if err != nil || len(values) < 2 {
			return fmt.Errorf("invalid POINT WKT")
		}
		return entity(write, "POINT", layer, values[0], values[1], 0, 0)
	}
	if strings.HasPrefix(upper, "LINESTRING") {
		values, err := numbers(wkt)
		if err != nil || len(values) < 4 || len(values)%2 != 0 {
			return fmt.Errorf("invalid LINESTRING WKT")
		}
		if err := write(0, "LWPOLYLINE"); err != nil {
			return err
		}
		if err := write(8, layer); err != nil {
			return err
		}
		if err := write(90, strconv.Itoa(len(values)/2)); err != nil {
			return err
		}
		for i := 0; i < len(values); i += 2 {
			if err := write(10, format(values[i])); err != nil {
				return err
			}
			if err := write(20, format(values[i+1])); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.HasPrefix(upper, "POLYGON") {
		values, err := numbers(wkt)
		if err != nil || len(values) < 6 || len(values)%2 != 0 {
			return fmt.Errorf("invalid POLYGON WKT")
		}
		if err := write(0, "LWPOLYLINE"); err != nil {
			return err
		}
		if err := write(8, layer); err != nil {
			return err
		}
		if err := write(90, strconv.Itoa(len(values)/2)); err != nil {
			return err
		}
		if err := write(70, "1"); err != nil {
			return err
		}
		for i := 0; i < len(values); i += 2 {
			if err := write(10, format(values[i])); err != nil {
				return err
			}
			if err := write(20, format(values[i+1])); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported geometry type")
}

func writeText(write func(int, string) error, label string, height float64) error {
	if err := write(0, "TEXT"); err != nil {
		return err
	}
	if err := write(8, "LABEL"); err != nil {
		return err
	}
	if err := write(10, "0"); err != nil {
		return err
	}
	if err := write(20, "0"); err != nil {
		return err
	}
	if err := write(40, format(height)); err != nil {
		return err
	}
	return write(1, label)
}

func entity(write func(int, string) error, kind, layer string, x, y, x2, y2 float64) error {
	if err := write(0, kind); err != nil {
		return err
	}
	if err := write(8, layer); err != nil {
		return err
	}
	if err := write(10, format(x)); err != nil {
		return err
	}
	if err := write(20, format(y)); err != nil {
		return err
	}
	if kind == "LINE" {
		if err := write(11, format(x2)); err != nil {
			return err
		}
		return write(21, format(y2))
	}
	return nil
}

func numbers(wkt string) ([]float64, error) {
	var result []float64
	for _, token := range strings.FieldsFunc(wkt, func(r rune) bool {
		return !(r == '-' || r == '+' || r == '.' || r >= '0' && r <= '9' || r == 'e' || r == 'E')
	}) {
		value, err := strconv.ParseFloat(token, 64)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}
func format(value float64) string { return strconv.FormatFloat(value, 'g', -1, 64) }
