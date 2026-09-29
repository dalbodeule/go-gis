# 커밋 준비 기록

이 문서는 현재 작업 트리를 사용자가 검토하고 직접 커밋하기 위한 요약이다.
에이전트는 저장소 규칙에 따라 commit/amend/rebase/push를 실행하지 않는다.

## 변경 범위

- GDAL/PROJ/GEOS/DXF/PostGIS native driver 경계와 round-trip 테스트
- 공용 filter, label, merge, spatial command 및 CLI/Lua 연결
- Qt Quick native desktop의 multi-layer, CRS 변환, 공통 extent 정규화,
  부분 chunk 렌더링, visibility-aware hit-test, 편집 저장 경로
- multi-geometry와 hole을 보존하는 render source 및 DXF 출력
- 대용량 render/hit-test benchmark와 race 검증
- OS별 build 문서, `all-native` 빌드 타깃, `scripts/verify.sh`, 3개 OS CI
- MIT `LICENSE`, 재배포 가능한 `testdata/sample.geojson`, ARES 검증 절차

## 로컬 검증

Qt와 native GIS 의존성이 설치된 환경에서 다음 명령이 모두 성공했다.

```sh
./scripts/verify.sh
```

추가로 샘플 DXF는 GDAL DXF driver로 재읽기했고, Qt 전용 패키지 테스트는
다음 명령으로 확인했다.

```sh
CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop
```

## 사용자 커밋 절차

먼저 설정을 확인한다.

```sh
git config --get user.signingkey
git config --get commit.gpgsign
```

변경 내용을 검토하고 staging한 뒤, 키 지문을 명령에 직접 지정하지 않고
로컬 Git 기본 키로 서명한다.

```sh
git diff --stat
git status --short
git add <검토한 파일들>
git commit -S -m "feat: complete GIS foundation and native render validation"
```

커밋 후에는 서명 상태를 확인한다.

```sh
git log -1 --show-signature
git status --short
```

## 외부 확인 잔여 항목

- ARES Commander 2027에서 UTF-8/CP949 DXF를 실제로 열고 한글·레이어·좌표를
  확인해야 한다. 절차는 [ARES 검증 문서](verification/ares-commander.md)에 있다.
- 각 배포 OS에서 GDAL/PROJ data와 DLL/shared library 패키징을 최종 확인해야 한다.
