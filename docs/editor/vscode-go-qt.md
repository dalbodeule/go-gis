# VS Code에서 Go native·Qt QML 진단 설정

이 저장소의 `cmd/gis-cli/filter_native.go` 등은 `//go:build native`, Qt 화면
코드는 `qt` 태그가 필요하다. `gopls` 기본 설정은 이 파일을 일반 빌드에서
제외하므로, native·Qt 코드를 함께 편집할 때 빌드 태그를 지정한다. QML은
Go 실행 파일에 포함되지만 Qt QML Language Server(`qmlls`)는 편집 시점에
`QtQuick` 모듈의 설치 경로를 따로 알아야 한다.

## 세 운영체제 공통 설정

설정 도구가 포함된 같은 작업트리를 각 머신에 준비하고, 저장소 **루트 폴더**를
VS Code로 연다. Go 1.27.3 이상과 현재 OS/CPU용
GDAL·PROJ·GEOS·Qt 6 개발 라이브러리를 설치한 뒤 같은 루트에서 실행한다.

```sh
go run -mod=readonly ./scripts/configure-vscode.go
```

이 명령은 Qt의 `qtpaths`에서 실제 `QT_INSTALL_QML` 값을 읽고
`QtQuick/qmldir`를 확인한다. 설치된 `qmlls`와 native GIS/Qt의
`pkg-config` 모듈도 확인한 뒤 **각 머신의** `.vscode/settings.json`을
만든다. 이 파일은 `.gitignore`에 포함되므로 macOS의 절대 경로가 Linux나
Windows로 전파되지 않는다. 이미 있는 JSON 설정의 다른 키는 보존한다.
설정을 쓰지 않고 결과만 보려면 `-dry-run`을 붙인다. `qtpaths` 또는
`qmlls`가 PATH에 없다면 `-qtpaths`와 `-qmlls`로 실행 파일 경로를 지정한다.
Qt 확장이 지원하는 **qmlls 6.8 이상**인지도 확인한다. 오래된 Qt를 앱 빌드에
사용하더라도 언어 서버만 새 버전으로 지정할 수 있다.

생성되는 주요 설정은 `gopls.buildFlags=-tags=native,qt`, C++17/CGO 환경,
Qt QML 모듈 경로, 이 저장소의 `qmltests` 모듈 경로, qmlls의 CMake 호출
비활성화다. 첫 QML 경로는 Qt 설치 모듈, 둘째는 `GoGIS 1.0` 테스트용
QML 모듈을 찾는다.

### macOS (Homebrew)

```sh
brew install go pkg-config gdal proj geos qt
qtpaths6 --query QT_INSTALL_QML
go run -mod=readonly ./scripts/configure-vscode.go
```

이 Mac에서는 QML 모듈이 `/opt/homebrew/share/qt/qml`에 설치되어 있다.

### Linux (Ubuntu 26.04 예시)

저장소가 요구하는 Go 버전을 설치하고 기존
[Linux 빌드 절차](../build.md)를 따른다. Qt 패키지와 QML 모듈을 설치한 뒤
QtQuick 경로를 확인한다.

```sh
sudo apt-get install -y build-essential pkg-config libgdal-dev libproj-dev libgeos-dev \
  qt6-base-dev qt6-declarative-dev qt6-tools-dev \
  qml6-module-qtquick qml6-module-qtquick-controls \
  qml6-module-qtquick-layouts qml6-module-qtquick-dialogs
qtpaths6 --query QT_INSTALL_QML
qmlls --version
go run -mod=readonly ./scripts/configure-vscode.go
```

Ubuntu 24.04의 기본 Qt는 qmlls 6.8 요구사항보다 오래될 수 있다. 이 경우
Qt 6.8 이상 SDK의 `qmlls`를 준비하고 `-qmlls /설치/경로/qmlls`로 지정한다.
`QtQuick/qmldir`가 없으면 배포판의 QtQuick QML 런타임 모듈을 확인한다.
패키지 이름과 Qt 버전은 배포판 버전에 따라 다르다.

### Windows (MSYS2 + PowerShell)

Go와 [Windows 패키징 workflow](../../.github/workflows/package-windows.yml)에
나열된 MSYS2 toolchain, GDAL·PROJ·GEOS·Qt 패키지를 설치한다. amd64는
UCRT64, arm64는 CLANGARM64 환경을 사용한다.
저장소 루트의 **PowerShell**에서 실행한다.

```powershell
$qtEnvironment = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -eq 'Arm64') { 'clangarm64' } else { 'ucrt64' }
$env:Path = "C:\msys64\$qtEnvironment\bin;C:\msys64\usr\bin;$env:Path"
go run -mod=readonly ./scripts/configure-vscode.go
```

MSYS2 설치 위치가 다르면 `MSYS2_ROOT` 환경 변수에 MSYS2 루트를 지정하거나
`-qtpaths`와 `-qmlls`에 해당 실행 파일을 지정한다. 설정 도구는
amd64에서 UCRT64 `gcc`·`g++`, arm64에서 CLANGARM64 `clang`·`clang++`,
그리고 `pkg-config`와 GIS 데이터 경로를 확인해 gopls 환경에 반영한다.
MSVC Qt와 MSYS2 라이브러리를 한 설정에서 섞지 않는다.

설정 후 VS Code에서 **Developer: Reload Window**를 실행한다. QML 경고가
남으면 **Qt: Restart QML Language Server**를 실행하고, Go 쪽 경고가 남으면
**Go: Restart Language Server**를 실행한다. Qt 확장 설정 화면의
“Additional Import Paths”와 “Custom Exe Path”에서도 같은 값을 확인할 수
있다. 확장이 다른 qmlls를 자동으로 내려받았다면 설치된 Qt와 맞는 실행 파일을
`customExePath`로 지정한다.

문제가 남으면 편집기 밖에서도 재현되는지 구분한다. macOS·Linux에서는:

```sh
CGO_CXXFLAGS=-std=c++17 go list -tags native,qt ./cmd/gis-cli ./cmd/gis-desktop
qmllint -I "$(qtpaths6 --query QT_INSTALL_QML)" \
  -I cmd/gis-desktop/qmltests cmd/gis-desktop/qml/Main.qml
```

Windows PowerShell에서는:

```powershell
go list -tags native,qt ./cmd/gis-cli ./cmd/gis-desktop
$qtpaths = (Get-Command qtpaths6.exe, qtpaths.exe -ErrorAction SilentlyContinue | Select-Object -First 1).Source
$qmlDir = (& $qtpaths --query QT_INSTALL_QML).Trim()
& qmllint.exe -I $qmlDir -I cmd/gis-desktop/qmltests cmd/gis-desktop/qml/Main.qml
```

Windows의 실제 QML 경로는 `qtpaths`의 질의 결과를 사용한다.

`qmllint`는 그 외의 기존 QML 코드 경고를 출력할 수 있다. 여기서는
`QtQuick`과 `GoGIS` 모듈을 찾지 못한다는 오류가 사라졌는지 확인한다.
실행 앱의 QML 검색 경로와 편집기의 검색 경로는 별개다.

참고: [gopls buildFlags](https://github.com/golang/tools/blob/master/gopls/doc/settings.md#buildflags),
[Qt 확장 QML Language Server 설정](https://doc.qt.io/vscodeext/vscodeext-how-to-turn-on-qmlls.html),
[Qt 확장 설정 목록](https://doc.qt.io/vscodeext/vscodeext-reference-qt-settings.html).
