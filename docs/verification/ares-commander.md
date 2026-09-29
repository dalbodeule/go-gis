# ARES Commander DXF 검증 절차

이 문서는 GoGIS exporter의 자동 검증과 ARES Commander 2027 실제 앱 검증을
구분한다. 현재 저장소와 CI에서 확인할 수 있는 것은 DXF 구조, 인코딩 바이트,
GDAL DXF parser round-trip까지이며, ARES의 화면 표시와 글꼴 대체는 ARES가
설치된 Windows 환경에서 수동으로 확인해야 한다.

## 자동 사전 검증

저장소 루트에서 아래 스크립트를 실행하면 두 profile을 임시 디렉터리에
생성하고, DXF header를 확인한다. `ogrinfo`가 설치되어 있으면 GDAL 재읽기도
추가로 수행한다.

```sh
./scripts/verify-ares-precheck.sh
```

샘플 GeoJSON을 UTF-8 DXF로 변환하고 GDAL이 geometry와 extent를 읽는지 확인한다.

```sh
go run -tags native ./cmd/gis-cli convert \
  --input testdata/sample.geojson \
  --output /tmp/gogis-ares-utf8.dxf \
  --source-crs EPSG:4326 --target-crs EPSG:4326 \
  --profile ares-utf8
ogrinfo -ro -al -so /tmp/gogis-ares-utf8.dxf
```

CP949 프로파일도 같은 방식으로 생성한다.

```sh
go run -tags native ./cmd/gis-cli convert \
  --input testdata/sample.geojson \
  --output /tmp/gogis-ares-cp949.dxf \
  --source-crs EPSG:4326 --target-crs EPSG:4326 \
  --profile ares-cp949
```

자동 테스트는 다음을 보장한다.

- `$ACADVER`와 `$DWGCODEPAGE`가 선택한 profile과 일치한다.
- UTF-8 레이블은 UTF-8 바이트로, CP949 레이블은 CP949 바이트로 기록된다.
- CP949로 표현할 수 없는 문자는 조용히 대체하지 않고 오류가 난다.
- Point, LineString, Polygon 및 TEXT entity가 DXF에 기록된다.
- GDAL DXF driver가 geometry 개수와 extent를 다시 읽는다.

## ARES Commander 수동 검증

1. Windows의 ARES Commander 2027에서 UTF-8 파일을 연다.
2. 파일을 열 때 오류·복구 대화상자가 없는지 확인한다.
3. `sample` geometry layer의 선과 폴리곤 위치가 원본 좌표와 일치하는지 확인한다.
4. `label` 명령으로 생성한 파일을 열고 `한글 도로`, `한글 건물`이 깨지지 않는지 확인한다.
5. 레이블의 위치, 높이, 회전, `Korean` text style이 유지되는지 확인한다.
6. CP949 파일도 열고 동일한 항목을 확인한다.
7. ARES에서 저장한 파일을 다시 열어 entity와 한글이 유지되는지 확인한다.

검증 기록에는 ARES 버전, Windows 버전, 사용한 profile, 파일 SHA-256,
결과(성공/실패), 오류 메시지와 화면 캡처 경로를 함께 남긴다. 실제 ARES
검증 결과가 추가되기 전까지 `ares-utf8`과 `ares-cp949`는 모두 실험적
profile이며 어느 한쪽을 호환성 정답으로 간주하지 않는다.
