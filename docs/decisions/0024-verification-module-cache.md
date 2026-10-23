# ADR 0024: 검증용 Go module cache 경로

- 상태: 적용
- 기준일: 2026-10-24
- 범위: `scripts/verify.sh`

## 결정

검증 스크립트는 기본적으로 Go가 선택한 기존 `GOMODCACHE`를 그대로 사용한다.
샌드박스나 CI에서 별도 경로가 필요하면 호출자가 `GOMODCACHE`를 지정할 수 있다.
`GOCACHE`는 기존처럼 저장소 검증용 기본 경로를 사용하고 환경 변수로 덮어쓸 수
있다.

## 이유

제한된 CI·샌드박스에서 사용자 module cache 접근이 막히는 경우에도 호출자가
쓰기 가능한 cache를 지정할 수 있어야 한다. 기본값을 강제로 바꾸지 않으면 이미
설치된 의존성이 있는 개발 환경의 오프라인 검증을 깨뜨리지 않는다.

## 검증

- `bash -n scripts/verify.sh`
- 임시 `GOCACHE`로 `go test ./...`, `go vet ./...`
- native 및 Qt desktop build
