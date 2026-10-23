# ADR 0023: QML CRS catalog 코드 인덱스

- 상태: 적용
- 기준일: 2026-10-24
- 범위: 프로젝트 CRS 선택 대화상자

## 결정

CRS catalog가 갱신될 때 EPSG 코드에서 catalog entry로 연결하는 JavaScript
인덱스를 한 번 만들고, 공통 목록·국가별 추천·선택 CRS 상세 조회에서 재사용한다.

## 이유

catalog entry 수가 커질수록 각 코드 조회마다 전체 배열을 선형 탐색하면 대화상자
열기와 목록 모델 평가 비용이 반복된다. catalog 교체 시에만 O(n) 인덱스를 만들고
일반 조회는 평균 O(1)로 유지한다. 원본 entry 객체와 검색 결과 semantics는
변경하지 않는다.

## 검증

- `./scripts/build.sh desktop-native`
- `git diff --check`
- QML CRS catalog 수용 테스트를 별도로 재실행한다.
