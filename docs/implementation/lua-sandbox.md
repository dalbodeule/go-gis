# Lua API 샌드박스 정책

GoGIS의 Lua runtime은 내부 Go 포인터나 UI 객체를 노출하지 않고, `gogis`
테이블의 명령 API만 제공한다.

## 허용 범위

- Lua base 문법과 `assert`, `pairs`, `ipairs`, `pcall` 등 순수 언어 기능
- 제한된 `table`, `string`, `math` 라이브러리
- `gogis.layers()`
- `gogis.set_property(layer, featureID, field, value)`
- `gogis.export_dxf(destination, layer[, profile])`. 이것은 Lua에 부여한 명시적
  호스트 파일쓰기 capability다. 스크립트는 현재 프로세스 권한으로 지정한 경로에
  파일을 생성하거나 덮어쓸 수 있으므로 신뢰할 수 없는 스크립트에 exporter를
  연결하지 않는다. `profile`은 `ares-utf8`
  (기본값) 또는 `ares-cp949`이며, 호출 중인 Go `context.Context`가 exporter에
  전달되어 취소가 포맷 출력까지 전파된다.
- `gogis.spatial(operation, left, right, result[, distance])`. `operation`은
  `intersect`, `union`, `difference`, `buffer` 중 하나이며, 내부적으로
  `commands.ApplySpatialOperation`과 동일한 dispatch를 사용한다. `buffer`는
  `right`를 비워 두고 `distance`를 사용한다.
- `gogis.filter(layer, field, value, result)`는 속성값이 일치하는 feature만
  복사한 결과 레이어를 생성한다.
- `gogis.filter_lua(source, result, predicate)`는 피처별 `feature` 속성 테이블을
  Lua predicate에 전달하고 `true`를 반환한 피처만 새 레이어에 복사한다.
  반환값이 boolean이 아니거나 스크립트가 실패하면 결과 레이어를 추가하지 않는다.
- `gogis.label(layer, field, result[, height, style])`는 속성값으로 라벨을
  생성하고 geometry 대표 위치에 배치한다.
- `gogis.label_lua(source, result, textScript[, ruleScript, height, style])`는
  Lua식으로 라벨 문자열을 합성한다. 선택적인 rule이 false이거나 textScript가
  빈 문자열/`nil`을 반환해도 feature는 결과 레이어에 남고 라벨만 비워 둔다.
- `filter_lua` 결과 레이어를 다음 filter의 입력으로 사용해 predicate 단계를
  체인으로 연결할 수 있다. `label_lua`는 원본의 모든 피처를 유지하므로 라벨
  조건에 걸리지 않은 피처도 공간 요소로 남는다.

예를 들어 도로 종류와 차선 수로 피처를 단계적으로 필터링한 뒤, 원본 도로의
라벨 표시 대상만 이름·차선 수 조합으로 라벨링할 수 있다.

```lua
gogis.filter_lua("roads", "primary_roads", [[
    return feature.CLASS == "primary"
]])
gogis.filter_lua("primary_roads", "wide_roads", [[
    return feature.LANES >= 4
]])
gogis.label_lua("roads", "roads_with_labels", [[
    return string.format("%s · %d차선", feature.NAME, feature.LANES)
]], [[
    return feature.CLASS == "primary" and feature.LANES >= 4
]], 2.5, "Korean")
```

## 차단 범위

`io`, `os`, `package`, `debug`, `coroutine`, `channel` 라이브러리는 열지
않는다. 파일 로딩·동적 모듈 로딩 함수(`dofile`, `loadfile`, `loadstring`,
`require`), `rawset`, stdout 출력 함수도 제거한다. `rawset`은 feature proxy의
`__newindex` 보호를 우회할 수 있어 닫는다. 따라서 스크립트가 임의의 파일을 읽거나
프로세스를 실행하거나 외부 Lua 모듈을 가져올 수 없다.

레이블/필터의 `feature`는 backing property table을 가리키는 보호된 proxy다.
`table.insert`, `table.remove`, `table.sort`는 내부에서 `__newindex` 없이 원본
table을 직접 바꿀 수 있으므로 proxy에 대해서만 오류를 발생시킨다. 스크립트가
직접 만든 일반 Lua table에는 이 표준 변경 함수들을 계속 사용할 수 있다.

`RunFile`의 스크립트 파일 읽기는 Go 호스트가 수행하는 동작이다. 호출자는
허용할 경로를 먼저 검증해야 하며, Lua 코드가 다른 파일을 다시 열 수 있는
권한을 의미하지 않는다.

`Run`/`RunFile`은 Lua 소스 크기를 1 MiB로 제한하고, `RunFile`은 제한된 reader로
읽는다. `Runtime.Run`은 최대 10분 실행 deadline을 적용하고, 호출자가 더 짧은
deadline을 주면 이를 따른다. 데스크톱 label/rule Lua도 한 번의 layer label 처리당
최대 10분이며, 각 feature의 Lua 평가는 별도 100 ms deadline을 적용한다. background
context로 직접 `LabelProgram`/`LabelComposerProgram`을 호출해도 비종료 루프는 deadline
error로 중단된다. `string.rep`와 `string.format`은 결과가 1 MiB를 넘을 수 있으면
할당 전에 거부하고, format의 동적 width/precision은 허용하지 않는다. 이 제한은 일부
명백한 할당 증폭을 막지만, 임의 Lua table 성장 등을 포함한 VM 전체 heap 사용량을
제한하는 hard memory quota는 아니다. 데스크톱의 준비된 label text는 feature당 64 KiB,
project 전체 128 MiB로 제한한다. 특히
GopherLua v1.1.2의 `LState.SetMx` 구현은 VM별 할당량이 아니라 프로세스 전체의
Go heap을 관찰하고 임계값에서 `os.Exit(3)`을 호출하므로 애플리케이션의 보안
quota로 사용하지 않는다. 해당 호출은 GoGIS에서 제거했다. 신뢰하지 않는 Lua를
실행해야 한다면 별도 프로세스, OS memory/CPU 제한, 허용된 export 디렉터리와
짧은 timeout을 적용해야 한다.

각 label/rule 평가에 전달되는 feature는 속성 65,536개 이하로 제한하며, Lua 테이블로
속성을 옮기는 전처리도 같은 100 ms context를 주기적으로 확인한다.

`string.byte`, `string.find`, `string.match`, `string.gmatch`, `string.gsub`는 비활성화한다. GopherLua의
패턴 함수는 VM instruction/context hook 바깥에서 실행되므로 timeout으로 중간 중단할 수
없으며, `byte`는 대형 속성 문자열 하나에서 수백만 개의 Lua 반환값을 만들 수 있고,
`gsub`는 매칭마다 전체 결과를 재복사한다. `table.concat`은 결과 길이를 1 MiB로
제한하고 사전 계량 후에만 할당하며, 계량 중 context 취소도 확인한다.

## 취소와 동시성

`Runtime.Run`과 데스크톱 label/rule 평가는 제한된 `context.Context`를 GopherLua
상태에 연결한다. 장시간 루프는 취소 또는 deadline으로 중단되며, 하나의 runtime은
Lua state 보호를 위해 한 번에 하나의 실행만 허용한다. label/rule의 개별 feature 평가도
최대 100 ms다. 메모리 quota는 없고, 10분
deadline도 의도적으로 긴 작업을 제한할 수 있다. 신뢰할 수 없는 사용자의 스크립트를
실행할 때는 별도 프로세스 격리와 OS 수준 memory/CPU limit을 추가해야 한다.

데스크톱 레이블 설정은 저장 전에 label/rule chunk를 컴파일해 문법 오류를
반환한다. 이 검증만으로 feature별 실행이나 결과 타입이 보장되지는 않는다.
실제 피처의 필드명·타입 오류와 렌더링 중 실행 오류도 계속 확인해야 한다.

## 대량 feature 성능 확인

Apple M3에서 composer 기반 Lua label/rule을 1,000,000회 평가하는 synthetic
benchmark는 약 1.42초(1,424 ns/feature), 누적 allocation 483 MB/12.4M allocations,
GC 뒤 heap 0.43 MiB로 측정됐다. 이 테스트는 10K property map을 재사용해 평가 경로만
측정하며 geometry 처리·label 객체 생성·Qt/GPU publish·실제 자료의 속성 크기는 포함하지
않는다. 누적 할당은 GC 뒤 heap보다 훨씬 크므로, 1M geometry/render end-to-end 성능의
대체 증거로 해석하지 않는다.
