# 현재 작업 트리 커밋 준비

이 문서는 2026-10-03 현재 미커밋 변경의 검토용 요약이다. 이전 마일스톤의
커밋 범위나 이미 완료한 검증을 이번 변경의 결과로 간주하지 않는다.
저장소 규칙에 따라 에이전트는 staging, commit, amend, rebase, push를 실행하지 않는다.

## 이번 변경 범위

- 대용량 읽기 전용 SHP: 원본을 건드리지 않는 임시 QIX 캐시(기본값), 공간창
  조회, 개요 경계의 GEOS dissolve, 줌 단계별 청크 크기 및 렌더 예산 조정.
  화면 표시용 근사 형상은 원본 geometry와 구분한다.
- 지도 뷰: 레이어 추가 시 현재 중심과 화면상 미터/픽셀을 유지하고,
  뷰포트 가로·세로 비율과 확대 상한을 보정한다. 여러 파일을 빠르게
  선택해도 요청을 순서대로 처리하고, 취소 시 대기 요청을 폐기한다.
- 레이어 UI: 레이어 제거 확인 및 원본 파일 보존, 우클릭 메뉴 닫기,
  속성창 정렬·번역, 사용 가능한 레이블 필드 힌트, 폴리곤 채움 없이
  경계선만 표시하는 설정을 추가한다.
- 회귀 테스트: GDAL/GEOS/render 및 Go–QML–C++ 브리지에서 실제 파일,
  빠른 연속 요청, 중심·축척 보존, 화면 청크 완료를 검사한다.

구현 결정과 사용법은 [ADR 0006](decisions/0006-progressive-overview-and-dxf-import.md),
[ADR 0007](decisions/0007-layer-lifecycle-and-metric-map-view.md),
[빌드 문서](build.md), [레이어 속성 문서](layer-properties.md)에 기록했다.
이전 마일스톤의 기반 작업은 각 결정 문서와 기존 커밋에서 확인한다.

## 검증 상태

2026-10-03 작업 중 `go test -tags 'qt native' ./...`, 관련
`go vet -tags 'qt native'`, 오프스크린 QML 테스트 27개,
`./scripts/build.sh desktop-native`, 표적 race 테스트를 통과했다.
실제 세종 연속지적도·도근점 SHP를 읽는 확대 렌더 회귀에서도
zoom 194.44와 840의 폴리곤 정점이 모두 안전 한도 안에 있었다.
측정값과 환경은 [후속 검증 기록](verification/deferred-user-validation.md)에 있다.
이 결과는 사용자의 실제 macOS 창에서 나타나는 픽셀, 체감 프레임 시간,
장시간 메모리 사용을 증명하지 않는다. 문서만 정리한 이번 턴에서는
전체 빌드·테스트를 다시 실행하지 않았다.

사용자 화면에서 다음을 별도로 확인해야 한다.

1. 두 세종 SHP를 빠르게 연달아 추가한 뒤 레이어가 모두 남고 현위치·축척이
   유지되는지 확인한다.
2. 확대·축소 중 연속지적도 경계가 사라지거나 양옆이 잘리지 않는지,
   전체범위 개요의 밀도와 메모리 사용이 적절한지 확인한다.
3. macOS 네이티브 스타일의 속성창 정렬, 필드 힌트, 경계선만 표시,
   레이어 제거와 원본 파일 보존을 확인한다.

Windows/Linux 패키징과 ARES Commander의 실제 DXF 확인도 여전히 별도
검증 항목이다. [ARES 검증 절차](verification/ares-commander.md)를 참고한다.

## 사용자 커밋 절차와 제안 메시지

변경 파일과 미추적 파일을 먼저 검토하고 필요한 파일만 직접 staging한다.
서명에는 로컬 Git 설정의 기본 GPG 키를 사용하며, 키 지문을 명령에
하드코딩하지 않는다. 설정이나 서명이 실패하면 커밋하지 않는다.

```sh
git status --short
git diff --stat
git config --get user.signingkey
git config --get commit.gpgsign
git add <검토한 파일들>
git commit -S -m "feat: improve large-layer rendering and desktop layer workflows"
git log -1 --show-signature
```

`git diff --stat`은 미추적 파일을 표시하지 않으므로 `git status --short`도
반드시 확인한다. 위 명령은 사용자가 검토 후 직접 실행하는 예시이며,
이 문서를 작성하면서 staging이나 커밋을 실행하지 않았다.
