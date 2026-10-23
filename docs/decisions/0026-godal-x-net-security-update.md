# Decision 0026: godal x/net 보안 수정 버전 상향

## 배경

`third_party/godal`에 대해 `govulncheck`가 `golang.org/x/net@v0.58.0`에서
HTTP/2 관련 취약점 5건을 보고했다. 수정 버전은 `v0.60.0`이다.

## 결정

godal의 간접 의존성을 `golang.org/x/net v0.60.0`으로 상향한다. 이 변경은
기존 공개 API를 바꾸지 않으며, 취약점이 보고된 HTTP/2 구현을 수정 버전으로
교체한다.

## 검증

- 로컬 Go 모듈 캐시에 `v0.60.0`이 존재하는 것을 확인했다.
- 네트워크가 허용되는 환경에서 `cd third_party/godal && govulncheck ./...`를
  다시 실행해 취약점 데이터베이스 기준 결과를 확인해야 한다.
- 이 환경에서는 `vuln.go.dev` DNS 접근이 차단되어 재스캔이 완료되지 않았다.
