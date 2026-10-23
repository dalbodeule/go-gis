module gogis

go 1.27

toolchain go1.27.3

require (
	github.com/airbusgeo/godal v0.0.18
	github.com/jackc/pgx/v5 v5.11.0
	github.com/mappu/miqt v0.15.0
	github.com/twpayne/go-geos v0.23.0
	github.com/twpayne/go-proj/v11 v11.1.0
	github.com/yuin/gopher-lua v1.1.2
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
)

replace github.com/airbusgeo/godal => ./third_party/godal

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.24.0 // indirect
)
