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

### 2026-10-05 검증 범위

| 환경 | 실제 확인한 범위 | 아직 확인하지 않은 범위 |
| --- | --- | --- |
| macOS ARM64 로컬 | Go 1.27.1; Qt 6.11.2; GDAL 3.13.3; PROJ 9.9.0; GEOS 3.15.0; `./scripts/build.sh all-native`; Qt/native 데스크톱 및 native 브리지 테스트 통과. 내부 `.app`/`.dmg` 생성, codesign verify 및 DMG CRC verify 통과 | 직접 실행 시 Cocoa plugin은 로드되나 Codex 세션의 pasteboard/XPC 연결 오류 후 SIGABRT; 일반 GUI 세션의 UI·GIS 데이터 검증과 clean-machine 테스트 필요 |
| macOS GitHub runner | portable checks, native GIS tests/race 및 native CLI build 통과 | Qt desktop build와 app packaging |
| Linux GitHub runner (Ubuntu) | portable checks, native GIS tests/race 및 native CLI build 통과 | Qt desktop build, AppImage/배포 패키지, 깨끗한 VM 실행 |
| Windows GitHub runner | portable tests/vet 및 portable CLI `.exe` build 통과 | native GIS/Qt desktop build, DLL 및 GIS/Qt runtime package |

세 OS의 최신 workflow 결과는 [2026-10-05 CI 실행](https://github.com/dalbodeule/go-gis/actions/runs/37301854222)에서 확인할 수 있다. CI 통과는 해당 job에 포함된 테스트/바이너리 범위만 증명한다. 특히 Windows CI의 portable CLI 빌드를 Windows native GIS 앱 빌드로 간주하지 않는다.

요청된 추가 배포 대상은 Windows/Linux의 `amd64`와 `arm64` 네 조합이다. 현재
CI에는 이 조합의 native GIS/Qt 데스크톱 패키지 job이 없다. `GOOS`/`GOARCH`만
지정한 CGO 비활성 CLI 교차 빌드는 이 요구를 충족하지 않으므로 배포 지원으로
표기하지 않는다. 각 조합은 해당 아키텍처 runner에서 Qt·GDAL·PROJ·GEOS를 같은
ABI로 빌드/설치하고, Qt 및 GIS runtime/data를 포함한 패키지를 만든 뒤 clean
machine에서 실행 검증해야 한다.

배포 목표 형식은 [ADR 0013](decisions/0013-desktop-distribution.md)에 따라 macOS `.app`/`.dmg`, Windows portable `.zip`(clean-machine 검증 후 installer), Linux 정의된 기준 배포판의 AppImage로 둔다. macOS ARM64 내부 테스트 산출물은 `build/packages/macos-arm64/`에 생성됐고 앱 번들 서명 및 DMG checksum을 검증했다. 직접 앱 실행은 Codex 세션의 AppKit pasteboard/XPC 연결 오류로 중단되어 UI runtime 검증은 미완료다. 패키지에는 Qt QML/plugin, GDAL/PROJ/GEOS 라이브러리와 data, 라이선스 고지가 필요하고 대상 OS에서 만든 뒤 clean-machine 실행을 통과해야 한다. 코드 서명/공증은 내부 검증 패키지와 공개 배포를 구별해 후속 gate로 둔다.

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
Qt desktop 실행 파일은 Qt SDK가 설치된 개발 환경에서 `scripts/verify.sh`가 테스트·빌드할 수 있습니다.
현재 CI는 Qt desktop build를 포함하지 않으며, 아래 표의 OS별 배포 번들을 생성하지 않습니다.

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

The Milestone B shell is optional and uses Qt 6.5 or newer Quick/QML through MIQT. It is
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

대용량 읽기 전용 모드는 GDAL 공간창을 viewport chunk 단위로 조회하며, 화면 밖의
geometry 전체를 Go 메모리에 적재하지 않는다. 제한된 뷰포트 청크를 요청하며,
넓은 영역이 화면에 맞는 개요 줌(zoom bucket 1 이하)에서는 복잡한 선·폴리곤에 GEOS의
topology-preserving display simplification을 적용한다. 이는 첫 화면이나 실사용 GPU 프레임 시간 최적화가
검증 완료됐다는 뜻은 아니다.
zoom bucket 1 이하에서는 폴리곤 원본 피처를 제한된 viewport 창에서 모두 읽은 뒤 GEOS로
coverage boundary를 dissolve한다. 따라서 개요에서도 필지 외곽 전체를 유지하면서 내부 공유
경계는 생략하고, 피처 선택용 geometry는 보유하지 않는다. 포인트 및 비폴리곤 피처는 확대
단계에 따라 결정적으로 표본화할 수 있다. 정확한 전체 형상과 선택은 zoom bucket 2 이상으로
확대하면 다시 활성화되며, 상태 표시줄에 근사 개요 모드임을 알린다. Dissolve는 청크별·읽기
전용 메모리 한도 안에서 수행되며 원본 geometry, 편집 데이터, 내보내기 데이터는 변경하지 않는다.
넓은 축척의 화면 형상과 경계는 근사일
수 있고, viewport의 feature/payload/native vertex 상한에 걸리면 로그에 불완전 렌더를
표시한다. 이는 전국 단위 전체 피처를 한 번에 메모리에 올리지 않도록 하는 정책이지,
모든 데이터를 한 화면에 완전히 표시한다는 보장은 아니다.

휠 확대 한도는 현재 데이터 범위와 지도 캔버스/뷰포트 비율에서 계산하며, 최소 25m 폭의
지도를 볼 수 있도록 한다. EPSG:4326은 데이터 범위 중심 위도에서 경도 폭을 미터로
근사한다. 그 밖의 투영 좌표계는 미터 단위 좌표를 전제로 하므로 다른 단위의 CRS는
표시 축척이 정확하지 않을 수 있다. zoom bucket 4보다 더 확대하면 공간 조회창을 단계적으로
줄여 상세 뷰에서 필요 이상의 주변 geometry를 읽지 않는다.

큰 SHP의 반복 공간 조회에는 QIX 공간 인덱스를 사용할 수 있다. 기본 설정은 피처 수
10,000개 이상인 viewport 기반 읽기 전용 SHP를 OS 임시 디렉터리에 복사한 뒤 QIX를
만드는 것이다. 원본 파일은 변경하지 않으며, 복사본은 원본 경로·구성 파일 크기·수정 시각을
기준으로 재사용한다. SHP와 DBF 등의 복사본만큼 임시 디스크 공간이 더 필요하다. 원본
폴더에 `.qix`만 생성하려면 `--spatial-index-location=source`를 지정한다. 이 모드는 원본
폴더 쓰기 권한이 필요하고, 원본 geometry/속성 파일은 수정하지 않는다. 실패하면 인덱스
없이 원본을 여는 경로로 계속한다.

```sh
./build/gogis-desktop-native --spatial-index-threshold=10000 --spatial-index-location=cache
./build/gogis-desktop-native --spatial-index-threshold=250000 --spatial-index-location=source
```

`--spatial-index-threshold=0` 또는 `--spatial-index-location=off`로 자동 생성을 끌 수 있다.
동일한 설정은 `GOGIS_SHAPEFILE_INDEX_THRESHOLD`와 `GOGIS_SHAPEFILE_INDEX_LOCATION`
환경변수로 전달할 수도 있으며, 명령행 값이 환경변수보다 우선한다. 기본값은 `10000`과
`cache`다. 임시 인덱스를 만들려면 GDAL이 사용하는 `.qix`가 SHP와 같은 폴더에 있어야
하므로 cache 모드는 sidecar만 따로 두지 않고 관련 SHP 구성 파일을 임시 폴더에 복사한다.

`desktop-native`는 `qt native` 태그로 GDAL 입력을 활성화하며, 입력 layer의
실제 이름을 QML 레이어 트리와 속성 테이블에 반영하며, layer 이름을 생략하면
dataset의 모든 layer를 로드합니다. `--save`를 지정하면 편집 Commit 결과를
새 GeoPackage/SHP로 저장하며, 다중 layer dataset에서는 현재 선택된 layer가
저장됩니다.

`--source-crs`는 입력 dataset의 CRS 메타데이터가 없거나 잘못 기록된 경우
모든 입력 layer에 적용하는 명시적 override입니다. `--target-crs`는 표시용
공통 CRS이며, 각 layer의 CRS가 다르면 PROJ로 변환합니다. CRS를 알 수 없는
layer를 변환 대상에 포함할 때는 `--source-crs`를 지정해야 합니다.

Desktop의 파일 열기/추가 대화상자에서 `.dxf`를 벡터 입력으로 선택할 수 있으며,
GDAL/OGR DXF driver의 읽기 지원 범위 안에서 entity를 가져온다. DXF는 좌표계 정보가
없는 경우가 많으므로 입력 좌표의 단위와 CRS를 확인해야 한다. 테스트용 UTF-8 도면은
GDAL을 통해 4개 entity로 읽히는 것을 검사한다. DXF exporter의 구조 검증은 GDAL DXF
driver로도 수행할 수 있습니다. 예를 들어 샘플 GeoJSON을 변환한 뒤 GDAL이 DXF를
다시 읽고 geometry 수와 extent를 인식하는지 확인합니다. 이 검사는 ARES Commander의 실제 화면·한글 글꼴
호환성을 대체하지 않으며, ARES 검증은 대상 앱에서 별도로 수행해야 합니다.

Native desktop에서는 **프로젝트 레이어를 DXF로 내보내기**로 프로젝트의 모든 레이어를
하나의 DXF에 내보냅니다. CAD 레이어명은 작업에서 설정한 표시 이름을 사용하며,
레이블은 해당 CAD 레이어에 기록합니다. 내보내기 전에 각 레이어의 Lua 표시 필터와
레이블을 계산합니다. 읽기 전용 viewport source는 레이어별로 전체 피처를 다시 읽으며,
각 레이어당 최대 1,000,000개 피처와 768 MiB의 디코딩된 geometry/attribute payload까지만 허용합니다. 한도를 넘으면
오류를 표시하고 부분 파일은 내보내지 않습니다.
저장 위치를 선택하기 전에 레이어별 포함 여부, CAD 레이어 이름, 도형과 레이블
내보내기 여부를 지정할 수 있습니다. 기본값은 모든 레이어의 도형·레이블을
내보내는 것입니다. 점은 POINT, 선과 폴리곤 경계는 LWPOLYLINE, 레이블은 TEXT가
됩니다. 불투명도가 있는 폴리곤은 구멍을 보존하도록 삼각분할한 DXF `SOLID`로
채우며, Z 좌표와 화면의 세부 심볼 스타일은 DXF에 기록하지 않습니다.

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

### Windows 포터블 패키지와 설치 프로그램

MSYS2 UCRT64가 `C:\tools\msys64`에 설치된 AMD64 PC에서는 다음과 같이 native
데스크톱 폴더와 ZIP을 만듭니다. 스크립트가 Go 바이너리를 빌드하고 `windeployqt`로
Qt/QML 파일을 수집한 뒤, 실행 파일과 Qt/GDAL 플러그인의 DLL 의존성을 MSYS2 prefix에서
추적해 패키지 폴더로 복사합니다. GDAL·PROJ 데이터, MSYS2 라이선스 파일, 빌드 정보도
함께 넣습니다. 패키지 실행 시 `resources\gdal`, `resources\proj` 경로를 자동으로
사용합니다.

```powershell
$env:PROJ_DATA = "C:\tools\msys64\ucrt64\share\proj"
$env:GDAL_DATA = "C:\tools\msys64\ucrt64\share\gdal"
$env:Path = "C:\tools\msys64\ucrt64\bin;C:\tools\msys64\usr\bin;$env:Path"
.\scripts\package_windows.ps1 -Arch amd64 -Version 0.1.0-dev
```

산출물은 `build\packages\windows\amd64\` 아래의 폴더와 ZIP입니다. 동일 버전의
출력물이 있으면 덮어쓰지 않고 중단합니다. Inno Setup 6 `ISCC.exe`가 설치된 경우
`-BuildInstaller`를 추가해 사용자 범위 설치 프로그램도 생성할 수 있습니다.

```powershell
.\scripts\package_windows.ps1 -Arch amd64 -Version 0.1.0-dev -BuildInstaller
```

ARM64는 Windows ARM64 native Go와 MSYS2 `CLANGARM64` 환경에서 빌드합니다. AMD64
UCRT64 DLL을 ARM64 실행 파일과 섞지 않습니다. 저장소의 수동 GitHub Actions workflow는
`windows-2025`/UCRT64와 `windows-11-arm`/CLANGARM64를 각각 사용해 포터블 ZIP과
Inno Setup 설치 파일을 생성합니다. Actions artifact는 14일 보관하며 공개 릴리스나
코드 서명은 하지 않습니다. Inno Setup 6 `ISCC.exe`가 필요합니다.
이 로컬 PC에는 UCRT64만 설치되어 있어 ARM64 바이너리와 ZIP은 ARM64 runner에서 별도로
실행해야 합니다.

```powershell
.\scripts\package_windows.ps1 -Arch arm64 -Version 0.1.0-dev
```

이는 package pilot 절차이며 clean Windows VM에서 GIS 데이터 열기, EPSG 변환, 저장,
DXF 내보내기 및 설치/제거를 통과하기 전에는 완성된 공개 배포로 간주하지 않습니다.
Windows ARM runner는 GitHub-hosted runner의 ARM64 label을 사용하고, MSYS2 ARM64
툴체인과 GDAL/PROJ/GEOS/Qt 패키지로 native 빌드합니다. MSYS2 ARM64 지원 및 Windows
Qt/GIS 런타임 조합은 실제 workflow 결과로 계속 확인해야 합니다.

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
OS별 portable 폴더, macOS 앱 번들, Windows 설치파일, Linux AppImage의 선택과
검증 게이트는 [데스크톱 배포 결정](decisions/0013-desktop-distribution.md)에 정리했다.

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
