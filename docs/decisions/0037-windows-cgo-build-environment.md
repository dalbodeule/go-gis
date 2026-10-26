# ADR 0037: Windows CGO 빌드 환경 선택

- 상태: 적용
- 기준일: 2026-10-26

## 결정

Windows `build.ps1`의 GIS/Qt 빌드에서 CGO를 활성화하고 C/C++ 컴파일러와
`pkg-config` 모듈을 빌드 전에 검사한다. 호출자가 컴파일러를 지정하지 않았고
PATH에서도 찾을 수 없을 때만 설치된 MSYS2 UCRT64(amd64) 또는
CLANGARM64(arm64)를 사용한다. `MSYS2_ROOT`, `C:\tools\msys64`,
`C:\msys64` 순으로 찾으며, 기존 `CC`/PATH 설정은 우선한다.

## 이유

Windows Go 1.27.3에서 C 컴파일러를 찾지 못하면 `CGO_ENABLED=0`이 될 수
있다. 이때 `native` 태그로 빌드하면 GDAL/PROJ/GEOS Go 바인딩의 CGO 파일이
제외되어 네이티브 라이브러리 오류 대신 Go `undefined` 오류가 발생한다.
현재 Windows 패키징은 MSYS2와 같은 ABI의 GIS/Qt 의존성을 사용한다.
개발 빌드도 같은 환경을 선택해 ABI 혼용을 줄이고, 누락된 의존성은 빌드
전에 알린다.
