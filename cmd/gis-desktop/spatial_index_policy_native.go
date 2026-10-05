//go:build qt && native

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type shapefileIndexPolicy struct {
	threshold int
	location  string
}

// Tests and direct runtime helpers keep indexing disabled unless desktop main
// explicitly configures the user-facing startup policy.
var activeShapefileIndexPolicy shapefileIndexPolicy

func configureShapefileIndexPolicy(args []string) {
	configureShapefileIndexPolicyWithDefaults(args, 10_000, "cache")
}

func configureShapefileIndexPolicyWithDefaults(args []string, threshold int, location string) {
	policy := shapefileIndexPolicy{threshold: threshold, location: location}
	if policy.threshold < 0 {
		policy.threshold = 10_000
	}
	if !validShapefileIndexLocation(policy.location) {
		policy.location = "cache"
	}
	if value := strings.TrimSpace(os.Getenv("GOGIS_SHAPEFILE_INDEX_THRESHOLD")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			policy.threshold = parsed
		} else {
			fmt.Fprintf(os.Stderr, "GoGIS: ignoring invalid GOGIS_SHAPEFILE_INDEX_THRESHOLD %q\n", value)
		}
	}
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("GOGIS_SHAPEFILE_INDEX_LOCATION"))); value != "" {
		if validShapefileIndexLocation(value) {
			policy.location = value
		} else {
			fmt.Fprintf(os.Stderr, "GoGIS: ignoring invalid GOGIS_SHAPEFILE_INDEX_LOCATION %q\n", value)
		}
	}
	for index := 0; index < len(args); index++ {
		name, value, hasValue := strings.Cut(args[index], "=")
		if !hasValue && index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			if name == "--spatial-index-threshold" || name == "--spatial-index-location" {
				index++
				value = args[index]
				hasValue = true
			}
		}
		if !hasValue {
			continue
		}
		switch name {
		case "--spatial-index-threshold":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				fmt.Fprintf(os.Stderr, "GoGIS: ignoring invalid --spatial-index-threshold %q\n", value)
				continue
			}
			policy.threshold = parsed
		case "--spatial-index-location":
			value = strings.ToLower(strings.TrimSpace(value))
			if !validShapefileIndexLocation(value) {
				fmt.Fprintf(os.Stderr, "GoGIS: ignoring invalid --spatial-index-location %q\n", value)
				continue
			}
			policy.location = value
		}
	}
	activeShapefileIndexPolicy = policy
}

func validShapefileIndexLocation(location string) bool {
	switch location {
	case "cache", "source", "off":
		return true
	default:
		return false
	}
}
