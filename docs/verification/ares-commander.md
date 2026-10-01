# ARES Commander DXF 검증 절차

이 문서는 GoGIS exporter의 자동 검증과 ARES Commander 2027 실제 앱 검증을
구분한다. 현재 저장소와 CI에서 확인할 수 있는 것은 DXF 구조, 인코딩 바이트,
GDAL DXF parser round-trip까지이며, ARES의 화면 표시와 글꼴 대체는 ARES가
설치된 지원 운영체제에서 수동으로 확인해야 한다.

Graebert의 2027 SP1 배포 정보에는 Windows 10/11과 macOS 14, 15, 26용 빌드가
기재되어 있고 macOS ARM 버전도 제공된다 ([공식 다운로드 페이지](https://www.graebert.com/cad-software/download/ares-commander/),
[2027 다운로드 및 시스템 요구사항](https://help.graebert.com/en/articles/14717586-download-links-of-ares-commander-2027)).
현재 검증 호스트는 macOS 27.2이며, 확인한 `/Applications`, 사용자 Applications,
Downloads 경로에서 ARES Commander 설치본을 찾지 못했다. 또한 27.2는 위에
게시된 macOS 요구사항에 포함되지 않으므로 이 호스트에서의 실제 앱 검증은
미실행으로 남긴다. Windows 10/11, 공식 요구사항에 포함된 macOS, 또는 ARES가
지원하는 Linux 배포판에서 아래 절차를 수행한다.

## 자동 사전 검증

저장소 루트에서 아래 스크립트를 실행하면 두 profile을 임시 디렉터리에
생성하고, DXF header를 확인한다. `ogrinfo`가 설치되어 있으면 GDAL 재읽기도
추가로 수행한다.

```sh
./scripts/verify-ares-precheck.sh
```

재배포 가능한 소형 입력(`testdata/sample.geojson`)에는 한글 도로와 건물 속성이
있다. 아래 명령은 해당 레이어의 `name` 필드로 높이 2.5, `Korean` 스타일의
TEXT를 생성한다. `angle` 필드는 도로 레이블을 30° 회전하며 건물은 0°로 둔다.
두 프로파일을 GDAL로 다시 읽는다. 보관 샘플은 다음 명령으로
재생성할 수 있다.

```sh
./scripts/generate-ares-samples.sh
```

생성된 파일은 `testdata/ares/sample-utf8.dxf`와
`testdata/ares/sample-cp949.dxf`이다. 자동 사전 검사는 UTF-8/CP949 한글
레이블, TEXT 개수, DXF header, geometry 및 extent round-trip을 검사한다.
현재 보관 샘플의 SHA-256은 아래와 같다(재생성 시 exporter 변경에 따라 달라질 수
있다).

| 파일 | SHA-256 |
| --- | --- |
| `sample-utf8.dxf` | `75e9325afdd626a505f5bcf5ef8ca02f6b8d919c55700c155d6eff8e9f0b720a` |
| `sample-cp949.dxf` | `63a7484b44cd9f55d4251f2f58f0c1711212bdf484512dfaf4e94f544f0c5a06` |

자동 테스트는 다음을 보장한다.

- `$ACADVER`와 `$DWGCODEPAGE`가 선택한 profile과 일치한다.
- UTF-8 레이블은 UTF-8 바이트로, CP949 레이블은 CP949 바이트로 기록된다.
- CP949로 표현할 수 없는 문자는 조용히 대체하지 않고 오류가 난다.
- Point, LineString, Polygon 및 TEXT entity가 DXF에 기록된다.
- 샘플에는 `한글 도로`, `한글 건물` TEXT가 각각 한 번씩 기록된다.
- 도로 TEXT에는 회전 30°(DXF group code 50)가 기록된다.
- GDAL DXF driver가 geometry 개수와 extent를 다시 읽는다.

## ARES Commander 수동 검증

1. 지원되는 OS의 ARES Commander 2027에서 `testdata/ares/sample-utf8.dxf`를 연다.
2. 파일을 열 때 오류·복구 대화상자가 없는지 확인한다.
3. `sample_labeled` layer의 선과 폴리곤 위치가 원본 좌표와 일치하는지 확인한다.
4. geometry와 함께 `한글 도로`, `한글 건물` 레이블이 깨지지 않는지 확인한다.
5. 도로 레이블의 30° 회전과 두 레이블의 위치, 높이(2.5), `Korean` text style을 확인한다.
6. `testdata/ares/sample-cp949.dxf`도 열고 동일한 항목을 확인한다.
7. ARES에서 저장한 파일을 다시 열어 entity와 한글이 유지되는지 확인한다.

검증 기록에는 ARES 버전, 운영체제 버전, 사용한 profile, 파일 SHA-256,
결과(성공/실패), 오류 메시지와 화면 캡처 경로를 함께 남긴다. 실제 ARES
검증 결과가 추가되기 전까지 `ares-utf8`과 `ares-cp949`는 모두 실험적
profile이며 어느 한쪽을 호환성 정답으로 간주하지 않는다.
