# 데스크톱 UI·문서·성능 점검 (2026-10-26)

기준은 [`desktop-gis-codex-brief.md`](../../desktop-gis-codex-brief.md)의
데스크톱 지도·속성·Lua 흐름이다. 실제 업무 데이터는 사용하지 않았다.

## 화면과 사용성

- macOS ARM64 Qt offscreen에서 960×640 창을 렌더했다. 수정 전에는 상단
  `저장·내보내기` 뒤의 정보·설정 버튼이 화면 밖으로 밀렸다. 수정 후 파일 추가,
  PostGIS, 작업공간, 속성, 저장·내보내기, 더보기 버튼이 창 안에 들어온다.
- QML 테스트는 최소 너비에서 메뉴 버튼의 위치와 정보·설정·프로젝트 좌표계
  대화상자 열기를 확인한다. 1360px에서 넓은 도구 모음으로 돌아가는 경계도 확인한다.
  한국어·영어·일본어 표시 설정을 각각 검사한다.
- 960×640 offscreen 화면에서 Lua 편집기, 레이어 속성, 속성 테이블, 앱 설정을
  열어 배치를 확인했다. 수정 전에는 모두 좌상단에 붙었고, 수정 후에는
  창 중앙에 제목과 확인/취소 버튼이 들어온다. 다른 modal 대화상자에도
  같은 배치 규칙을 적용했다.
- 앱 설정의 Shapefile 자동 인덱스 기준은 피처 수(정수)인데 로캘 기본 포맷이
  `10,000.00`을 표시했다. 소수점 0자리로 표시해 `10,000`으로 보이게 하고
  QML 테스트와 offscreen 화면에서 확인했다.
- 최소 높이 640px의 Lua 대화상자에서는 세 스크립트가 아래로 길게 쌓여
  마지막 편집기를 찾기 어려웠다. 표시 필터·레이블 규칙·레이블 문자를 탭으로
  분리하고 선택한 스크립트에만 코드 공간을 할당했다. 필드 삽입과 예제도
  선택 탭을 기준으로 동작한다. QML 테스트 68건에서 최소 높이의 세 탭과
  코드 영역 배치를 확인했다.
- OS별 실제 창 관리·폰트 크기·고해상도 배율은 offscreen 결과만으로
  확인할 수 없다. Windows/Linux 실기기 확인은
  [MVP 체크리스트](mvp-checklist.md)의 수동 항목으로 남긴다.

## 성능 측정

Apple M3 로컬, Go 1.27.3, `-benchtime=200ms`의 단일 실행 결과다.
측정치의 비교 범위는 각 benchmark 입력으로 한정한다.

| 경로 | 결과 | 확인한 사용 경로 |
| --- | --- | --- |
| 200행 속성 모델 `AttributeTableOwned` | 8.5 µs/op, 3.5 KiB/op, 2 allocs/op | 데스크톱의 분리된 속성 페이지 스냅샷을 QML payload로 만드는 경로 |
| 200행 속성 모델 `AttributeTable` | 27.3 µs/op, 70.7 KiB/op, 402 allocs/op | 호출자 소유 map을 분리해야 할 때의 복사 경로; 데스크톱 페이지에 적용하지 않음 |
| Lua 레이블 composer 1만 피처 | 14.6 ms/op, 피처당 약 1.47 µs, 4.8 MB/op | 합성식 평가만 측정; geometry·렌더·Qt 전송 제외 |

별도로 `TestWindowedReadOnlyRepeatedViewportMoves1M`을 실행해 100만 피처
GeoJSON에서 화면 이동 128회가 통과했다. 테스트 로그의 peak RSS는 188 MiB,
조회된 화면 피처와 레이블은 각각 누적 488개였다. 이 수치는 synthetic 입력과
해당 화면 이동 패턴의 결과이며 다른 SHP/GeoPackage·PostGIS나 최종 GUI
프레임 시간을 보증하지 않는다.

재현 명령:

```sh
QT_QPA_PLATFORM=offscreen qmltestrunner -import cmd/gis-desktop/qmltests -input cmd/gis-desktop/qmltests -silent
go test ./internal/presentation -run '^$' -bench '^BenchmarkAttributeTable(CompleteSchema200Rows|OwnedCompleteSchema200Rows)$' -benchmem -benchtime=200ms
go test ./internal/scripting -run '^$' -bench '^BenchmarkLabelComposerProgramEvaluate10KFeatures$' -benchmem -benchtime=200ms
GOGIS_TEST_REPEATED_VIEWPORT_1M=1 go test -tags 'qt native' ./cmd/gis-desktop -run '^TestWindowedReadOnlyRepeatedViewportMoves1M$' -count=1 -v
```
