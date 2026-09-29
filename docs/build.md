# GoGIS 빌드 가이드

이 문서는 GoGIS의 Go 코드와 GDAL/PROJ/GEOS 네이티브 의존성을 함께 빌드하는 방법을 설명합니다.

현재 저장소의 기본 CLI는 외부 GIS 라이브러리 없이도 빌드됩니다. `godal`, `go-proj/v11`, `go-geos`를 연결한 이후에는 각 운영체제에 맞는 개발 헤더와 라이브러리, CGO 툴체인이 필요합니다.

## 공통 요구사항

- Go 1.27 이상
- Git
- C 컴파일러와 링커
- `CGO_ENABLED=1`
- GDAL 3.0 이상 및 개발 헤더
- PROJ 9.4 이상 및 개발 헤더·resource data
- GEOS 및 개발 헤더
- `pkg-config` 또는 각 라이브러리의 include/library 경로 설정

## 빌드 스크립트와 산출물

모든 로컬 빌드 산출물은 저장소 루트의 `build/` 폴더에 둡니다. `build/`는
생성 디렉터리이므로 Git에서 추적하지 않습니다.

```sh
./scripts/build.sh cli       # build/gis-cli
./scripts/build.sh native    # build/gis-cli-native
./scripts/build.sh desktop   # build/gogis-desktop
./scripts/build.sh all       # cli + desktop
./scripts/build.sh clean     # build/ 제거
```

`desktop` 대상은 Qt 6의 C++17 요구사항을 위해 `CGO_CXXFLAGS`에
`-std=c++17`을 자동으로 추가합니다. 호출자가 이미 `-std=c++17` 또는
`-std=gnu++17`을 지정한 경우 기존 값을 유지합니다.

Go 바인딩은 다음 모듈을 사용합니다.

```text
github.com/airbusgeo/godal
github.com/twpayne/go-proj/v11
github.com/twpayne/go-geos
```

모듈을 추가한 뒤에는 반드시 실제 설치된 네이티브 라이브러리 버전과 Go 모듈이 요구하는 버전을 함께 확인합니다.

이 저장소에서는 네이티브 바인딩 파일을 `native` build tag로 분리합니다. 일반적인 문서·코어 테스트는 외부 GIS 라이브러리 없이 실행하고, 네이티브 검증 때만 다음처럼 태그를 지정합니다.

```sh
go test -tags native ./drivers/native ./...
go build -tags native -o bin/gis-cli ./cmd/gis-cli
```

현재 `drivers/native`에는 세 바인딩의 import와 초기화/생성 경계가 들어 있습니다. GDAL/PROJ/GEOS 개발 라이브러리가 설치되지 않은 환경에서 `-tags native`를 사용하면 의도적으로 CGO 헤더/링커 오류가 발생합니다.

## macOS

Apple Silicon과 Intel 모두 Homebrew 경로를 먼저 확인합니다.

```sh
brew update
brew install go pkg-config gdal proj geos

export PATH="$(brew --prefix)/bin:$PATH"
export PKG_CONFIG_PATH="$(brew --prefix gdal)/lib/pkgconfig:$(brew --prefix proj)/lib/pkgconfig:$(brew --prefix geos)/lib/pkgconfig:${PKG_CONFIG_PATH:-}"

gdalinfo --version
projinfo --version
geos-config --version
pkg-config --modversion gdal
pkg-config --modversion proj
pkg-config --modversion geos_c
```

Apple Silicon의 기본 Homebrew prefix는 보통 `/opt/homebrew`, Intel Mac은 `/usr/local`입니다. `brew --prefix` 결과를 사용하므로 경로를 직접 하드코딩하지 않습니다.

빌드:

```sh
export CGO_ENABLED=1
go test ./...
go vet ./...
go build -o bin/gis-cli ./cmd/gis-cli
```

## Qt Quick desktop prototype

The Milestone B shell is optional and uses Qt 6 Quick/QML through MIQT. It is
guarded by the `qt` build tag, so the standard CLI and test commands do not
need Qt. Install Qt 6 development components for Core, Gui, Quick, Qml, and
QuickControls2, then make sure the Qt `pkg-config` files and a CGO-compatible
C/C++ compiler are visible in the same shell.

```sh
CGO_CXXFLAGS=-std=c++17 go run -tags qt ./cmd/gis-desktop
```

The first prototype renders the desktop shell and keeps the map canvas as an
explicit hand-off point for the custom scene-graph item. The UI-neutral chunk
scheduler is tested by the normal Go test suite. A Qt build cannot be verified
on a machine without the Qt development installation; in that case use
`go test ./...` to verify the scheduler and the rest of the repository.

실행 시 PROJ grid/resource data를 찾지 못하면 다음을 확인합니다.

```sh
export PROJ_DATA="$(brew --prefix proj)/share/proj"
export GDAL_DATA="$(brew --prefix gdal)/share/gdal"
```

## Linux

### Debian/Ubuntu

```sh
sudo apt-get update
sudo apt-get install -y \
  build-essential pkg-config \
  libgdal-dev gdal-bin \
  libproj-dev proj-bin proj-data \
  libgeos-dev
```

검증:

```sh
gdalinfo --version
projinfo --version
geos-config --version
pkg-config --modversion gdal
pkg-config --modversion proj
pkg-config --modversion geos_c
```

### Fedora/RHEL 계열

패키지 이름은 배포판 버전에 따라 다를 수 있습니다.

```sh
sudo dnf install -y \
  gcc gcc-c++ make pkgconf-pkg-config \
  gdal gdal-devel \
  proj proj-devel \
  geos geos-devel
```

빌드:

```sh
export CGO_ENABLED=1
go test ./...
go vet ./...
go build -o bin/gis-cli ./cmd/gis-cli
```

배포 환경에서 resource data를 별도 경로에 설치했다면 다음을 설정합니다.

```sh
export PROJ_DATA=/usr/share/proj
export GDAL_DATA=/usr/share/gdal
```

## Windows

Windows는 Go, CGO 컴파일러, GIS 네이티브 라이브러리의 ABI가 모두 일치해야 합니다. macOS/Linux에서 만든 바이너리에 Windows DLL을 나중에 복사하는 방식은 지원하지 않습니다.

### 권장 개발 셸

1. 64-bit Go를 설치합니다.
2. MSYS2 UCRT64 또는 Visual Studio Build Tools 중 프로젝트의 CGO 툴체인을 하나로 선택합니다.
3. GDAL·PROJ·GEOS의 헤더와 라이브러리를 같은 ABI/아키텍처로 설치합니다.
4. `gcc`, `pkg-config`, `gdal`, `proj`, `geos`가 같은 셸에서 검색되는지 확인합니다.

PROJ 공식 문서는 Windows에서 OSGeo4W를 가장 간단한 설치 경로로 안내합니다. OSGeo4W를 사용할 경우 OSGeo4W Shell에서 개발 도구와 라이브러리를 설치하고 그 셸 안에서 빌드합니다.

```bat
gdalinfo --version
projinfo --version
pkg-config --modversion gdal
pkg-config --modversion proj
pkg-config --modversion geos_c
```

OSGeo4W 또는 별도 설치 경로가 `pkg-config`에 자동으로 등록되지 않으면 `PKG_CONFIG_PATH`에 `lib\pkgconfig` 경로를 추가합니다. 경로는 설치한 prefix에 맞게 바꿉니다.

```bat
set CGO_ENABLED=1
set PKG_CONFIG_PATH=C:\OSGeo4W\lib\pkgconfig
go test ./...
go vet ./...
go build -o bin\gis-cli.exe .\cmd\gis-cli
```

vcpkg를 선택하는 경우에는 GDAL·PROJ·GEOS를 동일한 triplet으로 설치하고, Go가 사용하는 C 컴파일러와 같은 ABI를 선택해야 합니다. `x64-windows`와 MinGW 계열 triplet을 섞지 않습니다. vcpkg 설치 경로를 `PKG_CONFIG_PATH`, `CGO_CFLAGS`, `CGO_LDFLAGS`에 연결하는 작업은 설치 방식에 따라 달라지므로 고정된 경로를 저장소에 넣지 않습니다.

실행 시 DLL과 resource data가 필요합니다. 실행 파일과 같은 디렉터리 또는 `PATH`에 GDAL/PROJ/GEOS DLL을 두고, PROJ/GDAL data 경로를 설정합니다.

```bat
set PROJ_DATA=C:\OSGeo4W\share\proj
set GDAL_DATA=C:\OSGeo4W\share\gdal
```

## 의존성 확인 스크립트

네이티브 드라이버를 활성화한 뒤에는 다음 명령이 모두 성공해야 합니다.

```sh
go version
go env GOOS GOARCH CGO_ENABLED
gdalinfo --version
projinfo --version
geos-config --version
pkg-config --modversion gdal
pkg-config --modversion proj
pkg-config --modversion geos_c
go test ./...
go vet ./...
```

`geos-config`가 제공되지 않는 배포판에서는 `pkg-config --modversion geos_c`를 사용합니다.

## 빌드 산출물과 런타임 패키징

네이티브 라이브러리는 실행 파일에 자동으로 정적으로 포함된다고 가정하지 않습니다.

- macOS: 필요한 `.dylib`와 GDAL/PROJ data를 앱 번들 또는 설치 prefix에 포함
- Linux: 배포판 패키지 의존성으로 설치하거나 호환되는 `.so`와 data directory를 패키징
- Windows: 호환되는 `.dll`과 GDAL/PROJ data를 함께 배포

배포 전에는 다음을 실제 대상 OS에서 확인합니다.

1. SHP의 `.shp/.shx/.dbf/.prj`를 열 수 있는가
2. GeoPackage를 열고 저장할 수 있는가
3. EPSG:5179, EPSG:5186, EPSG:4326 변환과 필요한 grid data가 재현되는가
4. GEOS 연산 후 C heap 메모리가 정상적으로 해제되는가
5. DXF 파일을 ARES Commander 2027에서 열고 한글·레이어·좌표가 유지되는가

## 교차 컴파일 주의

CGO 기반 GIS 드라이버가 활성화된 상태에서 `GOOS`/`GOARCH`만 바꿔 교차 컴파일하지 않습니다. 대상 OS/아키텍처용 GDAL·PROJ·GEOS 개발 라이브러리와 C 툴체인이 필요하므로 macOS, Linux, Windows 각각의 CI runner 또는 네이티브 빌드 환경에서 빌드합니다.

외부 의존성을 사용하지 않는 현재 CLI 뼈대만 교차 컴파일하려면 다음처럼 할 수 있지만, 이는 GIS 드라이버가 포함된 정식 배포 빌드가 아닙니다.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/gis-cli-linux-amd64 ./cmd/gis-cli
```

## 참고 문서

- [GDAL Download and installation](https://gdal.org/en/latest/download.html)
- [PROJ Installation](https://proj.org/en/stable/install.html)
- [PROJ resource files](https://proj.org/en/stable/resource_files.html)
- [GEOS installation](https://libgeos.org/usage/install/)
- [godal installation notes](https://github.com/airbusgeo/godal#installation)
- [go-proj installation notes](https://github.com/twpayne/go-proj#install)
- [go-geos installation notes](https://github.com/twpayne/go-geos#install)
