# ADR 0021: 선택적 빌드 플래그의 안전한 처리

- 상태: 적용
- 기준일: 2026-10-24
- 범위: `scripts/build.sh`의 Qt 데스크톱 빌드

## 결정

`GOGIS_VERSION`이 설정되지 않은 기본 빌드에서는 빈 linker flag 배열을
parameter expansion으로 전달하지 않고, 플래그가 있는 경우와 없는 경우를
명시적으로 분기한다.

## 이유

빌드 스크립트가 `set -u`를 사용하면 Bash 버전에 따라 빈 배열의
`"${array[@]}"` 확장이 unbound variable 오류를 일으킬 수 있다. 버전 문자열을
지정한 빌드의 `-ldflags` 동작은 유지하면서, 기본 CLI/Qt/native 빌드도 동일한
스크립트로 재현 가능해야 한다.

## 검증

- `bash -n scripts/build.sh`
- `./scripts/build.sh native`
- `./scripts/build.sh desktop-native`
- `./scripts/verify.sh`의 일반·race·native test 및 native build 단계
