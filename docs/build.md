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
./scripts/build.sh all-native # build/gis-cli-native + build/gogis-desktop-native
./scripts/build.sh all       # cli + desktop
./scripts/build.sh clean     # build/ 제거
```

커밋 전 전체 검증은 다음 명령으로 실행합니다. 일반/native 테스트와 race
검사, `go vet`, native 산출물 빌드, patch whitespace 검사를 순서대로 수행합니다.

```sh
./scripts/verify.sh
```

Windows PowerShell에서는 같은 portable 검증을 다음처럼 실행할 수 있습니다.
`-Native`와 `-Qt`는 해당 SDK와 native 라이브러리가 설치된 경우에만 추가합니다.

```powershell
.\scripts\verify.ps1
.\scripts\verify.ps1 -Native -Qt
```

GitHub Actions는 Windows에서 PowerShell portable CLI 빌드와 portable Go 테스트·vet을 수행하고, Linux/macOS
에서는 GDAL·PROJ·GEOS native 테스트와 native race 테스트를 추가로 수행합니다.
Qt 데스크톱 패키징은 각 OS의 Qt 배포 방식 차이 때문에 CI native GIS job과
분리하며, Qt가 설치된 개발 환경에서 `scripts/verify.sh`가 수행합니다.

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

데스크톱 폴리곤 채움은 GEOS constrained Delaunay API를 사용하므로 native
빌드에 GEOS 3.10 이상이 필요합니다. 설치 후 `geos-config --version` 또는
`pkg-config --modversion geos`로 버전을 확인합니다. 해당 API는 GEOS 3.10에서
추가되었습니다([GEOS 3.10 릴리스 노트](https://libgeos.org/posts/2021-10-01-geos-3-10-released/)).

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
pkg-config --modversion geos
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

The layer-properties QML interaction test uses an offscreen mock map canvas and
requires Qt Quick Test (`qmltestrunner`):

```sh
QT_QPA_PLATFORM=offscreen qmltestrunner \
  -import cmd/gis-desktop/qmltests \
  -input cmd/gis-desktop/qmltests
```

This verifies that the dialog's Apply button submits its settings payload. It
does not replace native GDAL/Qt integration tests or the Windows desktop
interaction checklist.

저장소 빌드 스크립트는 데모 UI와 native 데이터 UI를 구분합니다.

```sh
./scripts/build.sh desktop
./scripts/build.sh desktop-native
./build/gogis-desktop-native --input testdata/sample.geojson --layer sample \
  --source-crs EPSG:4326 --target-crs EPSG:5179 \
  --save build/sample-edited.gpkg
```

데스크톱에서 100,000개 이상의 feature를 가진 데이터 또는 개수를 확인할 수 없는
데이터를 열면 메모리 사용을 줄이기 위해 자동으로 읽기 전용으로 전환합니다.
geometry-only 초기 로딩과 원본 GDAL attribute page 조회를 사용하며, 상태 표시줄에
전환 이유가 표시됩니다. `--editable-large`는 자동 read-only 전환을 건너뛰고 전체 편집
snapshot을 시도하지만, reader 안전 상한(최대 100,000 feature/128 MiB)은 그대로 적용됩니다.
따라서 이 옵션은 1M feature 레이어를 편집 가능하게 만들지 않으며, 한도를 넘으면 안전하게
오류 처리됩니다. 대형 데이터는 기본 read-only viewport 모드로 여는 것을 권장합니다.
편집·저장이 필요 없는 대용량 시각화는 `--read-only`로도 직접 실행할 수 있습니다.

```sh
./build/gogis-desktop-native --read-only --input data/large.gpkg
./build/gogis-desktop-native --editable-large
```

이 모드에서는 편집 Commit과 `--save`를 사용하지 않으며, 속성은 페이지를
넘길 때 필요한 행만 읽습니다.
50,000개 이상의 feature를 가진 단일 CRS 입력은 첫 2,000개 feature를
미리 표시한 뒤 전체 geometry로 교체합니다. 혼합 CRS 또는 재투영이 필요한
입력은 정확한 전체 범위를 유지하기 위해 미리보기를 생략합니다.

현재 읽기 전용은 속성 맵과 중복 프로젝트 스냅샷을 피하지만 전체 geometry를
메모리에 적재한다. GDAL `OpenWindowGeometryOnly`는 드라이버 API로 구현되어
있으나 desktop viewport/chunk 렌더러에는 아직 연결하지 않았다. 그러므로 이 모드는
전국 단위 데이터의 OOM 방지나 화면 영역만 로드하는 기능을 의미하지 않는다.

`desktop-native`는 `qt native` 태그로 GDAL 입력을 활성화하며, 입력 layer의
실제 이름을 QML 레이어 트리와 속성 테이블에 반영하며, layer 이름을 생략하면
dataset의 모든 layer를 로드합니다. `--save`를 지정하면 편집 Commit 결과를
새 GeoPackage/SHP로 저장하며, 다중 layer dataset에서는 현재 선택된 layer가
저장됩니다.

`--source-crs`는 입력 dataset의 CRS 메타데이터가 없거나 잘못 기록된 경우
모든 입력 layer에 적용하는 명시적 override입니다. `--target-crs`는 표시용
공통 CRS이며, 각 layer의 CRS가 다르면 PROJ로 변환합니다. CRS를 알 수 없는
layer를 변환 대상에 포함할 때는 `--source-crs`를 지정해야 합니다.

DXF exporter의 구조 검증은 GDAL DXF driver로도 수행할 수 있습니다. 예를
들어 샘플 GeoJSON을 변환한 뒤 GDAL이 DXF를 다시 읽고 geometry 수와 extent를
인식하는지 확인합니다. 이 검사는 ARES Commander의 실제 화면·한글 글꼴
호환성을 대체하지 않으며, ARES 검증은 대상 앱에서 별도로 수행해야 합니다.

```sh
go run -tags native ./cmd/gis-cli convert \
  --input testdata/sample.geojson \
  --output /tmp/gogis-check.dxf \
  --source-crs EPSG:4326 --target-crs EPSG:4326 \
  --profile ares-utf8
ogrinfo -ro -al -so /tmp/gogis-check.dxf
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
pkg-config --modversion geos
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

PowerShell에서는 저장소의 Windows 전용 스크립트를 사용합니다. 산출물은
`build\`에 생성됩니다.

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\build.ps1 cli
.\scripts\build.ps1 native
.\scripts\build.ps1 desktop
.\scripts\build.ps1 all-native
.\scripts\build.ps1 clean
```

`native`, `desktop-native`, `all-native`는 아래 native GIS/Qt 의존성과 CGO
툴체인이 설치된 Windows 환경에서만 실행할 수 있습니다.

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
pkg-config --modversion geos
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

### Windows 대용량 로딩 기준 측정

네이티브 의존성 및 Qt 빌드가 통과한 뒤 같은 셸에서 다음 benchmark를 실행합니다.
fixture는 테스트가 임시 디렉터리에 생성하며 실제 업무 데이터를 사용하지 않습니다.
서로 다른 OS의 절대 시간보다 같은 Windows 환경에서의 쌍 비교와 할당량을
우선 기록합니다.

```powershell
go test -tags native ./drivers/gdal -run '^$' -bench 'BenchmarkGDALOpen(VectorOnly)?GeoJSON10KPoints|BenchmarkAttributeSessionOpen(All)?GeometryOnlyMultiLayerGeoPackage' -benchtime=10x -count=3 -benchmem
$env:CGO_CXXFLAGS = '-std=c++17'
go test -tags 'qt native' ./cmd/gis-desktop -run '^$' -bench 'BenchmarkDesktop(GDALSnapshot|RenderSources|ReadOnlyLoad)GeoJSON10K' -benchtime=3x -count=3 -benchmem
go test -tags 'qt native' ./cmd/gis-desktop -run '^$' -bench 'BenchmarkDesktopReadOnly(Load|Preview)GeoJSON50K' -benchtime=5x -count=2 -benchmem
```

실제 파일에서는 최초 표시 시간, pan/zoom 응답, 최대 메모리 사용량도 따로
측정합니다. synthetic benchmark만으로 UI 체감 성능을 판단하지 않습니다.
실제 데스크톱 실행 시 `GOGIS_PERF=1`을 설정하면 로드 시작을 기준으로
`vertices-published`와 Qt `scenegraph` geometry 반영 시간·버텍스 수를
표준 오류에 출력합니다. `stage=preview`가 `stage=full`보다 먼저
`scenegraph`에 나타나는지 확인할 수 있습니다. 이 시각은 화면 present 완료가
아니므로 실제 첫 화면 표시도 별도로 관찰해야 합니다.

```powershell
$env:GOGIS_PERF = '1'
.\build\gogis-desktop-native.exe --read-only --input C:\data\large.gpkg
```

같은 파일의 미리보기 없는 기준값은 `$env:GOGIS_DISABLE_PREVIEW = '1'`을
추가하고 다시 실행해 비교합니다. 측정 후 두 환경변수를 제거하면 기본
미리보기 동작으로 돌아갑니다.

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
pkg-config --modversion geos
go test ./...
go vet ./...
```

`geos-config`가 제공되지 않는 배포판에서는 `pkg-config --modversion geos_c`를 사용합니다.

## 빌드 산출물과 런타임 패키징

네이티브 라이브러리는 실행 파일에 자동으로 정적으로 포함된다고 가정하지 않습니다.

- macOS: 필요한 `.dylib`와 GDAL/PROJ data를 앱 번들 또는 설치 prefix에 포함
- Linux: 배포판 패키지 의존성으로 설치하거나 호환되는 `.so`와 data directory를 패키징
- Windows: 호환되는 `.dll`과 GDAL/PROJ data를 함께 배포

### 단일 실행 파일 배포 점검

현재 빌드 스크립트는 배포 패키지가 아니라 실행 파일만 `build/`에 만든다.
macOS ARM64에서 2026-10-01 확인한 산출물은 다음과 같다.

| 산출물 | 크기 | 검사 결과 | 의미 |
| --- | ---: | --- | --- |
| `build/gis-cli` (기본) | 2.5 MiB | OS 시스템 라이브러리 외 GIS shared library 없음 | 단일 CLI 파일은 가능하지만 GDAL/PROJ/GEOS 기반 명령은 native build 필요 오류를 반환 |
| `build/gis-cli-native` | 5.5 MiB | GDAL 3.13, PROJ 9.9, GEOS C shared library에 동적 링크 | 해당 dylib와 GDAL/PROJ data가 별도로 필요 |
| `build/gogis-desktop-native` | 54 MiB | Qt Widgets/Gui/Core/Qml/Quick와 GDAL/PROJ/GEOS에 동적 링크 | Qt framework, platform/QML plugin, GIS dylib와 data가 별도로 필요 |

검사 근거는 `file`, `du -h`, `otool -L` 결과다. `cmd/gis-desktop/main_qt.go`는
`Main.qml`을 Go 실행 파일에 embed하지만 Qt 런타임과 QML import plugin까지
embed하지는 않는다. 같은 점검은 Linux에서 `ldd`, Windows Developer Command
Prompt에서 `dumpbin /dependents`로 반복한다. PROJ grid/resource와 GDAL data는
현재 별도 경로를 사용한다.

따라서 지금 제공되는 단일 파일은 “native GIS 기능이 빠진 portable CLI”에
한정된다. 전체 GIS 기능을 제공하는 native CLI와 Qt desktop은 단일파일 배포로
검증되지 않았다. 권장 기본 산출물은 OS별 앱/배포 폴더이며 Qt framework/plugin,
GDAL/PROJ/GEOS shared library와 data를 포함해야 한다. Qt static build는 기술적으로
가능한 구성도 있지만 plugin/QML import를 정적으로 포함하고 재빌드해야 하며,
현재 빌드 설정에는 없다. Qt의 LGPL/GPL/commercial licensing 조건과 사용자 재링크
권리는 선택한 모듈과 배포 형태별로 별도 검토한다 ([Qt licensing](https://doc.qt.io/qt-6/licensing.html),
[Qt LGPL obligations](https://www.qt.io/development/open-source-lgpl-obligations)).
GDAL은 MIT 기반이지만 GDAL binary의 optional drivers/dependencies는 별도 라이선스
조건을 가질 수 있다 ([GDAL license](https://gdal.org/en/stable/license.html)).

현재 single-file audit의 상세 상태와 배포 전에 사람의 개입이 필요한 검증은
[보안·배포·사용자 후속 확인](verification/deferred-user-validation.md)에 모았다.

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
