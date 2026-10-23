# ADR 0020: 레이어 이름 조회 인덱스

- 상태: 적용
- 기준일: 2026-10-24
- 범위: `ProjectService`의 feature/property/geometry 조회

## 결정

`ProjectService`는 프로젝트 snapshot별로 레이어 이름을 slice 위치에 매핑하는
지연 생성 인덱스를 유지한다. feature ID 인덱스와 같은 snapshot identity를
사용하며, 편집 commit/rollback 또는 레이어 구성이 바뀌면 함께 무효화한다.

## 이유

feature ID 인덱스가 이미 준비된 이후에도 `FeatureProperty`와 `FeatureGeometry`가
레이어 이름을 매번 선형 탐색했다. 레이어가 많은 프로젝트의 반복 선택·속성 조회
경로에서 불필요한 O(layer count) 비용이 발생하므로, 이름 lookup을 평균 O(1)로
줄이고 기존 feature ID 인덱스의 lazy-build semantics는 유지한다.

중복 이름은 프로젝트 생성 단계에서 거부되며, 방어적으로 인덱스가 중복을 만나면
첫 번째 레이어를 유지한다. 반환 geometry의 clone 및 속성의 소유권 규칙은 변경하지
않는다.

## 검증

- 다중 레이어에서 property/geometry 조회와 인덱스 위치를 회귀 테스트한다.
- `go test ./...`, `go vet ./...`, `git diff --check`를 실행한다.
