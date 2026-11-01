# ADR 0050: 현 렌더링 경로의 GPU·SIMD 검토

- 상태: 검토 완료 (2026-11-01)
- 기준: `desktop-gis-codex-brief.md` 3절의 성능 우선순위

지도 캔버스는 이미 Qt Quick `QSGGeometryNode`에 삼각형 정점 버퍼를 공급한다.
플랫폼별 GPU 백엔드는 Qt RHI가 선택한다. 따라서 GPU 가속을 새로 추가하기보다
정점 생성, QSG 업로드, 실제 draw 시간 중 어느 구간이 느린지 분리 측정해야 한다.

기존 대형 SHP 벤치마크 기록(ADR 0047, 0048)은 GDAL 공간 조회, 필터 평가,
표시할 geometry 생성에 큰 비용이 있음을 보여 준다. 특히 화면에 도형이 없는
청크의 조회 회피가 효과를 보였다. 이 작업 중 Windows amd64 개발 환경에서
`BenchmarkSchedulerRunMissing1000`은 3회 기준 약 0.677 ms/op,
199,410 B/op, 1,017 allocs/op였다. 이는 스케줄러 단위 벤치마크이며
GDAL 조회·정점 생성·Qt/GPU 프레임 시간은 포함하지 않는다.

현 단계에서는 별도 SIMD 어셈블리 분기나 GPU 백엔드 교체를 적용하지 않는다.
SIMD 대상으로 보이는 단순 좌표/범위 계산보다 GDAL 호출, 메모리 할당,
도형 디코딩의 비중이 크다는 기존 계측이 근거다(ADR 0032, 0047, 0048).
다음 성능 작업은 동일 데이터·뷰포트에서 다음 시간을 따로 기록한 뒤 결정한다:
GDAL read, 필터, WKB 해독·정점 생성, Go→Qt 전달, `updatePaintNode`, GPU frame.
실제 GPU 프레임 시간이 지배적일 때만 버퍼 갱신량·draw call을 먼저 줄이고,
좌표 계산이 지배적임이 확인될 때만 플랫폼별 SIMD를 비교한다.
