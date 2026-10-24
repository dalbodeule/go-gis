# Linux·Windows amd64의 CSV SIMD 경로 검증

이 문서는 [ADR 0032](../decisions/0032-milestone-d-boundary-performance-and-simd.md)의
CSV 텍스트 검증 변경을 실제 x86-64 CPU에서 확인하는 절차다. 목적은 기능·네이티브
GIS 연동·성능을 구분해 확인하는 것이다. macOS ARM64 측정치와 운영체제 사이의
절대 시간을 직접 비교하지 않는다. 테스트에 사용하는 CSV는 저장소의 합성 데이터이며
업무 데이터나 연결 정보를 추가하지 않는다.

## 공통 기준

1. Linux와 Windows 각각 **실제 amd64 호스트**에서 같은 소스 버전과 Go 버전을
   사용한다. `GOOS=linux GOARCH=amd64` 교차 컴파일이나 에뮬레이션은 기능·속도
   검증을 대신하지 못한다.
2. Go, OS/CPU, `GOAMD64`, GDAL·PROJ·GEOS·Qt 버전과 테스트 결과를 기록한다.
   `CGO_ENABLED=1`과 동일 아키텍처의 C/C++ 컴파일러·라이브러리를 사용한다.
3. 기능 검증이 통과한 뒤 같은 호스트에서 벤치마크를 반복한다. 이때 백그라운드
   작업을 최소화하고, 첫 실행과 반복 실행의 편차를 함께 보관한다. `ns/op`와
   `B/op`, `allocs/op`을 기록하며 짧은 문자열과 긴 문자열을 구분한다.
4. 긴 입력칸의 `byte-search`가 `rune-scan`보다 빨라야 한다. 전체 가져오기 시간은
   GC·디스크·CPU 상태에 민감하므로 한 번의 수치나 OS 간 수치로 회귀를 판정하지
   않는다. 같은 호스트에서 변경 전후를 각각 5회 측정하고 중앙값을 비교한다.

## Linux amd64 (Ubuntu 24.04 예시)

저장소 루트에서 실행한다. Qt 테스트와 QML 테스트를 포함하려면 아래 Qt 패키지도
설치한다. `./scripts/verify.sh`는 Qt의 `pkg-config` 모듈이 없으면 Qt 테스트를
건너뛰므로, 전체 검증 전 존재 여부를 확인한다.

```sh
uname -m                         # x86_64
go version
go env GOOS GOARCH GOAMD64 CGO_ENABLED
sudo apt-get update
sudo apt-get install -y build-essential libgdal-dev libproj-dev libgeos-dev \
  qt6-base-dev qt6-declarative-dev qt6-tools-dev pkg-config
pkg-config --modversion gdal proj geos Qt6Core Qt6Quick Qt6Qml
pkg-config --exists Qt6Core Qt6Quick Qt6Qml
export CGO_ENABLED=1
export QT_QPA_PLATFORM=offscreen
./scripts/verify.sh
```

성공 기준은 일반·native 테스트, race 검사, `go vet`, native 빌드, Qt native
테스트, 100만 도형 뷰포트 스트레스, `git diff --check`의 통과다. `qmltestrunner`가
설치되어 있으면 QML 테스트도 실행되므로 출력의 건너뛰기 여부를 기록한다.

## Windows amd64 (PowerShell + MSYS2 UCRT64)

64비트 Windows에 `go.mod`가 요구하는 Go와 MSYS2 UCRT64를 설치한다. MSYS2
UCRT64 셸에서 다음 패키지를 설치한다. 패키지 구성은
[Windows 패키징 workflow](../../.github/workflows/package-windows.yml)의
amd64 job과 같다.

```sh
pacman -Syu
pacman -S --needed mingw-w64-ucrt-x86_64-toolchain \
  mingw-w64-ucrt-x86_64-gdal mingw-w64-ucrt-x86_64-proj \
  mingw-w64-ucrt-x86_64-geos mingw-w64-ucrt-x86_64-pkgconf \
  mingw-w64-ucrt-x86_64-qt6-base mingw-w64-ucrt-x86_64-qt6-declarative \
  mingw-w64-ucrt-x86_64-qt6-tools
```

그 뒤 **저장소 루트의 PowerShell**에서 같은 UCRT64 도구와 DLL 경로를 사용한다.
MSYS2 설치 위치가 다르면 `$ucrt`와 `$msysBin`을 실제 경로로 바꾼다.

```powershell
$ucrt = 'C:\msys64\ucrt64'
$msysBin = 'C:\msys64\usr\bin'
$env:Path = "$ucrt\bin;$msysBin;$env:Path"
$env:CGO_ENABLED = '1'
$env:CC = "$ucrt\bin\gcc.exe"
$env:CXX = "$ucrt\bin\g++.exe"
$env:CGO_CXXFLAGS = '-std=c++17'
$env:PKG_CONFIG_PATH = "$ucrt\lib\pkgconfig"
$env:PROJ_DATA = "$ucrt\share\proj"
$env:GDAL_DATA = "$ucrt\share\gdal"
$env:QT_QPA_PLATFORM = 'offscreen'
[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture  # X64
go version
go env GOOS GOARCH GOAMD64 CGO_ENABLED
& "$ucrt\bin\pkg-config.exe" --modversion gdal proj geos Qt6Core Qt6Quick Qt6Qml
.\scripts\verify.ps1 -Native -Qt
```

`verify.ps1`은 일반·native 테스트와 race 검사, vet, CLI 빌드, nested GODAL,
Qt native 테스트, 100만 도형 뷰포트 스트레스 및 공백 검사를 수행한다.
`-SkipRace`를 붙인 실행은 전체 검증으로 기록하지 않는다. Windows에서
`qmltestrunner.exe`가 설치되어 있다면 Qt UI 테스트를 별도로 실행한다.

```powershell
& "$ucrt\bin\qmltestrunner.exe" -import cmd/gis-desktop/qmltests -input cmd/gis-desktop/qmltests -v1
if ($LASTEXITCODE -ne 0) { throw 'QML test failed' }
```

## 두 OS에서 동일하게 실행할 성능 측정

다음 명령은 두 OS의 저장소 루트에서 동일하게 사용한다. PowerShell에서도 작은따옴표
인수를 그대로 사용할 수 있다. 먼저 정확성 테스트를 다시 실행한 뒤 긴 입력칸
검색, CSV 100만 행 가져오기, GEOS 경계 분리의 시간을 측정한다.

```sh
go test ./internal/commands -run 'TestImportCSVPointsKeepsEarlierRowsAfterRecordReuse|TestCSVTextRejectsReplacementCharacter|TestCSVImportReportsUndecodableText' -count=1
go test ./internal/commands -run '^$' -bench '^BenchmarkReplacementSearchLongField$' -benchtime=1000000x -count=5 -benchmem
go test ./internal/commands -run '^$' -bench '^BenchmarkImportCSVPoints1M$' -benchtime=3x -count=5 -benchmem
go test ./internal/commands -run '^$' -bench '^BenchmarkBuildBoundaryHulls(100k|Convex10k)$' -benchtime=3x -count=5 -benchmem
go test -tags native ./drivers/geos -run '^$' -bench '^BenchmarkSplitPolygonBoundaries10000x(32|1024)$' -benchtime=3x -count=5 -benchmem
```

선택적으로 `GOAMD64=v1` 또는 `v2` 빌드에서 같은 머신의
`GODEBUG=cpu.avx2=off` 결과를 비교할 수 있다. `GOAMD64=v3` 이상은 AVX2를
필수 기능으로 취급하므로 이 옵션으로 비활성화하지 않는다. 옵션은 Go 런타임의
선택적 CPU 기능만 제어하며 GDAL·GEOS 같은 별도 네이티브 라이브러리에는
적용되지 않는다. 옵션 사용 결과는 기본 실행 결과와 구별해 기록한다.

결과 기록 양식:

| 항목 | Linux amd64 | Windows amd64 |
| --- | --- | --- |
| 소스 버전, Go 버전, CPU, `GOAMD64` |  |  |
| GDAL / PROJ / GEOS / Qt 버전 |  |  |
| 전체 검증 및 QML 테스트 결과 |  |  |
| 긴 입력칸 `byte-search` / `rune-scan` 중앙값 |  |  |
| 100만 행 가져오기 `ns/op`, `B/op`, `allocs/op` 중앙값 |  |  |
| 10만 점 그룹 경계 / 1만 외곽점 경계 중앙값 |  |  |
| 1만 면 × 32 / 1,024 경계 중앙값 |  |  |

현재 [일반 CI](../../.github/workflows/verify.yml)는 Linux native GIS 테스트를
수행하지만 Windows는 portable 검증만 실행한다. Windows native·Qt 검증은
위 PowerShell 절차를 직접 실행하거나 별도 CI job으로 추가해야 한다.
[Windows 패키징 workflow](../../.github/workflows/package-windows.yml)의
앱 빌드 성공만으로 테스트 통과를 대신하지 않는다.
