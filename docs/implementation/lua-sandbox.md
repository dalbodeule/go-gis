# Lua API 샌드박스 정책

GoGIS의 Lua runtime은 내부 Go 포인터나 UI 객체를 노출하지 않고, `gogis`
테이블의 명령 API만 제공한다.

## 허용 범위

- Lua base 문법과 `assert`, `pairs`, `ipairs`, `pcall` 등 순수 언어 기능
- `table`, `string`, `math` 표준 라이브러리
- `gogis.layers()`
- `gogis.set_property(layer, featureID, field, value)`
- `gogis.export_dxf(destination, layer)`

## 차단 범위

`io`, `os`, `package`, `debug`, `coroutine`, `channel` 라이브러리는 열지
않는다. 파일 로딩·동적 모듈 로딩 함수(`dofile`, `loadfile`, `loadstring`,
`require`)와 stdout 출력 함수도 제거한다. 따라서 스크립트가 임의의 파일을
읽거나 프로세스를 실행하거나 외부 Lua 모듈을 가져올 수 없다.

`RunFile`의 스크립트 파일 읽기는 Go 호스트가 수행하는 동작이다. 호출자는
허용할 경로를 먼저 검증해야 하며, Lua 코드가 다른 파일을 다시 열 수 있는
권한을 의미하지 않는다.

## 취소와 동시성

`Runtime.Run`은 전달받은 `context.Context`를 GopherLua 상태에 연결한다.
장시간 루프는 context 취소로 중단되며, 하나의 runtime은 Lua state 보호를
위해 한 번에 하나의 실행만 허용한다. 메모리·실행 시간의 절대 quota는 아직
없으므로 신뢰할 수 없는 사용자의 스크립트를 실행할 때는 별도 프로세스
격리와 OS 수준 resource limit을 추가해야 한다.
