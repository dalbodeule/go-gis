# ADR 0025: QML Bound delegate의 model role 명시

- 상태: 적용
- 기준일: 2026-10-24
- 범위: CRS 선택 목록 delegate

## 결정

`pragma ComponentBehavior: Bound`를 사용하는 CRS `ItemDelegate`에 `modelData`와
`index`를 `required property`로 선언한다.

## 이유

암시적 delegate context에 의존하면 offscreen QML 테스트와 일부 Qt 6 환경에서
`modelData`가 `ReferenceError`가 되어 목록 표시·클릭 처리·선택 CRS 갱신이
실패한다. model role을 명시하면 delegate 재사용과 비동기 배열 갱신에서도
binding이 안정적으로 유지된다.

## 검증

- CRS catalog QML 테스트 통과
- 전체 QML 테스트: 62건 통과, 실패 0건
- Qt native 데스크톱 빌드 및 `git diff --check`
