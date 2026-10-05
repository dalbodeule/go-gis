# ARES Commander DXF 검증 절차

이 문서는 GoGIS exporter의 자동 검증과 ARES Commander 2027 실제 앱 검증을
구분한다. 현재 저장소와 CI에서 확인할 수 있는 것은 DXF 구조, 인코딩 바이트,
GDAL DXF parser round-trip까지이며, ARES의 화면 표시와 글꼴 대체는 ARES가
설치된 지원 운영체제에서 수동으로 확인해야 한다.
GDAL은 UTF-8 DXF의 `$DWGCODEPAGE` 값만으로는 UTF-8을 인식하지 못해 한글을
ANSI_1252로 잘못 디코딩할 수 있다. 이 경우 GDAL 전용 읽기 설정
`DXF_ENCODING=UTF-8`을 주면 실제 3-레이어 작업공간의 레이어명과 엔티티 수가
DXF 원문과 모두 일치했다. 이 설정은 ARES가 요구하는 DXF 파일 형식을 바꾸는
것이 아니라 GDAL 검증기의 해석을 지정한다. 근거는
[GDAL DXF 드라이버 설명](https://gdal.org/en/stable/drivers/vector/dxf.html)이다.

2026-10-05 조사에서 기존 `Downloads/test.dxf`(23 MiB, 77,360개 entity)가
`$DWGCODEPAGE`에 잘못된 그룹 코드 `1`을 사용하고, R2000 entity의 핸들·소유
블록·서브클래스 및 참조 TABLES를 기록하지 않는 것을 확인했다. GDAL은 이 파일을
읽었지만, 이러한 명세 불일치는 사용자가 보고한 ARES 복구 경고의 유력한 원인이다.
exporter는 코드 페이지 그룹 `3`, 선언된
LAYER/STYLE/BLOCK_RECORD, 고유 entity 핸들과 서브클래스를 쓰도록 변경했다.
TEXT에는 DXF 명세의 두 `AcDbText` 서브클래스 구간을 모두 기록한다
([Autodesk TEXT 그룹 코드](https://help.autodesk.com/cloudhelp/2024/ENU/AutoCAD-DXF/files/GUID-62E5383D-8A14-47B4-BFC4-35824CAE8363.htm)).
포인트 심볼은 DXF `$PDMODE=35`, `$PDSIZE=-1`로 원과 십자 모양을 지정해
기본 단일 픽셀 표시보다 눈에 띄도록 했다. Windows에서 HATCH 경계 수정을 적용한
뒤에도 건물 채움이 나타나는 순간 ARES 렌더링이 멈춰, Native 데스크톱에서는
채워진 폴리곤을 GEOS로 삼각분할하고 DXF `SOLID`로 기록한다. 원본 외곽선과
구멍 경계는 `LWPOLYLINE`으로 유지한다. 삼각분할은 구멍 영역을 채우지 않는다
([Autodesk SOLID 그룹 코드](https://help.autodesk.com/cloudhelp/2018/ENU/AutoCAD-DXF/files/GUID-E0C5F04E-D0C5-48F5-AC09-32733E8848F2.htm),
[GEOS constrained Delaunay](https://libgeos.org/doxygen/geos__c_8h.html)).
한글 레이블 스타일은 Windows의 Malgun Gothic 파일 `malgun.ttf`를 지정한다
([Microsoft 글꼴 목록](https://learn.microsoft.com/en-us/typography/font-list/malgun-gothic)).
CAD에서 255자까지의 레이어 이름과 공백을 허용하도록 `$EXTNAMES=1`도 명시하고,
금지 문자·길이는 내보내기 전에 검사한다. 이 선택은 Autodesk의
[EXTNAMES 설명](https://help.autodesk.com/cloudhelp/2027/ENU/AutoCAD-Core/files/GUID-8EC065EC-D551-4E02-9C5A-A33D1DB80B05.htm)과
[레이어 이름 규칙](https://help.autodesk.com/cloudhelp/2021/ENU/AutoCAD-LT/files/GUID-28E52FA3-248E-4F65-94DD-7C47BE761D58.htm)을 따른다.
프로젝트 내보내기에는 `$VIEWCTR`와 `$VIEWSIZE`도 기록한다. 초기 화면은 선택한
레이어 범위의 중앙을 사용하고, 선·면 레이어가 있으면 점 레이어 범위는 초기
축척 계산에서 제외해 고립된 측량점이 전체 도면을 지나치게 축소하지 않게 한다.
ARES에서 헤더 값만으로는 초기 화면이 원점에 가까워, VPORT 테이블의 `*ACTIVE`
레코드에도 같은 중심과 높이를 기록한다. VPORT는 현대 DXF 리더에서 헤더 변수보다
우선한다 ([Autodesk VPORT 헤더 변수](https://help.autodesk.com/cloudhelp/2024/ENU/AutoCAD-DXF/files/GUID-ED26E626-BC45-4256-9914-87E5FFE934B8.htm),
[VPORT 그룹 코드](https://help.autodesk.com/cloudhelp/2023/ENU/AutoCAD-DXF/files/GUID-8CE7CC87-27BD-4490-89DA-C21F516415A9.htm)).
또한 UTF-8은 AutoCAD 2007 DXF(`AC1021`), CP949는 AutoCAD 2000
DXF(`AC1015`)로 구분한다. 이는 명세 위반을 제거한 것이며, ARES의 복구 경고가
사라졌는지는 아래 수동 재검증이 필요하다. 기존 파일은 자동으로 고쳐지지 않으므로
새 빌드에서 다시 내보내야 한다.
출력은 같은 디렉터리의 임시 파일에 완성한 후 최종 경로로 교체하므로 오류·취소
시 이전 파일을 불완전한 DXF로 덮어쓰지 않는다.

대용량 사전 검증에서는 기존 `test.dxf`를 새 exporter로 임시 재출력했고, GDAL이
새 파일의 77,360개 entity와 원본과 동일한 extent를 다시 읽었다. 이는 데이터
손실을 찾지 못했다는 증거이지 ARES의 복구 경고가 사라졌다는 증거는 아니다.
재출력 파일의 전체 77,360개 `LWPOLYLINE`을 그룹 코드 쌍으로 다시 검사한 결과,
선언된 정점 수(90)와 X/Y 좌표 쌍(10/20)의 불일치, 엔티티 핸들 중복, 모델 공간
소유자(330) 또는 서브클래스(100) 누락은 모두 0건이었다. 이전 파일은 같은
77,360개 폴리라인에 핸들과 서브클래스가 전혀 없었다. 이 검사는 DXF 내부 구조
일부에 대한 검증이며 ARES 렌더링·복구 동작의 대체 검증은 아니다.
그룹 코드 `3` 및 UTF-8 버전 선택의 근거는 Autodesk의
[HEADER 변수 표](https://help.autodesk.com/cloudhelp/2021/ENU/AutoCAD-DXF/files/GUID-A85E8E67-27CD-4C59-BE61-4DC9FADBE74A.htm)와
[DXF 문자열 인코딩 규칙](https://help.autodesk.com/cloudhelp/2021/CHS/AutoCAD-DXF/files/GUID-2553CF98-44F6-4828-82DD-FE3BC7448113.htm)이다.

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
`testdata/ares/sample-cp949.dxf`, 그리고 세 레이어를 가진
`sample-project-utf8.dxf`·`sample-project-cp949.dxf`이다. 뒤의 두 파일은
작은 합성 도형만 사용하며 실제 업무 데이터를 포함하지 않는다. 건물에는 SOLID 삼각형이,
점에는 POINT와 한글 TEXT가 들어간다. 건물 샘플은 구멍이 있는 폴리곤과 분리된
MultiPolygon 구성요소를 포함해 채움에서 구멍이 비어 있는지 눈으로 확인할 수 있다.
자동 사전 검사는 UTF-8/CP949 한글 레이블,
TEXT 개수, DXF header, geometry 및 extent round-trip을 검사한다.
현재 보관 샘플의 SHA-256은 아래와 같다(재생성 시 exporter 변경에 따라 달라질 수
있다).

| 파일 | SHA-256 |
| --- | --- |
| `sample-utf8.dxf` | `8b0bc5d41d8b343bedcecb60324630939954ad5a0ea83d6c4c6ad3a57a5f6d3f` |
| `sample-cp949.dxf` | `fdadb149f2e46445e4564e27d7e07e79fb3ffaf7a3b93d6fe23745df23c08942` |
| `sample-project-utf8.dxf` | `fec1fb558af2e26c461b4920bd32ce2f4b4c5526810a53015cd44a21b2a7936c` |
| `sample-project-cp949.dxf` | `f103c33588e707cca7e7ac49b581b30571a824cb8a774d7ca95afef0b418ec8f` |

자동 테스트는 다음을 보장한다.

- `$ACADVER`와 `$DWGCODEPAGE`(그룹 코드 `3`)가 선택한 profile과 일치한다.
- 프로젝트 DXF의 `$VIEWCTR`/`$VIEWSIZE`와 `*ACTIVE` VPORT가 실제 비점 레이어
  범위를 중심으로 일치한다.
- 각 엔티티는 고유 핸들, 모델 공간 소유자, 타입별 서브클래스를 갖고 참조하는
  LAYER/STYLE/BLOCK_RECORD가 정의된다.
- POINT 심볼 크기와 모양 header가 설정되고, 한글 STYLE은 `malgun.ttf`를 가리킨다.
- Native 데스크톱의 채우기 설정 폴리곤은 구멍을 제외한 SOLID 삼각형과 원본
  외곽/내부 경계 LWPOLYLINE을 쓴다.
- UTF-8 레이블은 UTF-8 바이트로, CP949 레이블은 CP949 바이트로 기록된다.
- CP949로 표현할 수 없는 문자는 조용히 대체하지 않고 오류가 난다.
- Point, LineString, Polygon 및 TEXT entity가 DXF에 기록된다.
- 샘플에는 `한글 도로`, `한글 건물` TEXT가 각각 한 번씩 기록된다.
- 도로 TEXT에는 회전 30°(DXF group code 50)가 기록된다.
- GDAL DXF driver가 geometry 개수와 extent를 다시 읽는다.

## ARES Commander 수동 검증

프로젝트 내보내기는 활성 레이어 하나가 아닌 모든 프로젝트 레이어를 같은 DXF에
기록한다. CAD 레이어명은 프로젝트의 표시 이름을 사용하고 TEXT도 해당 레이어에
배치한다. 연속지적도·지적도근점·건물의 세 레이어를 가진 실제 작업에서 세 이름과
도형 수를 확인한다. 여러 레이어를 쓰는 자동 테스트와 GDAL 재읽기는 ARES 화면
검증을 대체하지 않는다.

로컬 실제 3-레이어 작업공간을 `GOGIS_TEST_DXF_WORKSPACE`로 지정해 실행한
선택적 통합 테스트에서는 DXF 그룹과 GDAL 재읽기를 비교하여 `0-연속지적도` 418,442,
`0-지적도근점` 23,942, `0-건물` 328,176개 엔티티를 확인했다. 건물은 외곽선과
SOLID 삼각형을 포함한다. 지적도근점은 POINT와 TEXT를 포함하며, 테스트는 사용자 데이터를
저장소에 복사하지 않고 출력 파일도 임시 디렉터리에 생성한다.
세부 타입 집계는 건물 LWPOLYLINE 77,360개와 SOLID 250,816개(HATCH 0개), 도근점 POINT 11,971개와
TEXT 11,971개였다. 도근점 필드값 11,971개 전부에 대한 레이블이 기록된 것을 확인했다.
같은 작업공간을 CP949 프로필로 다시 내보낸 결과도 GDAL이 별도 인코딩 재정의
없이 세 한글 레이어명과 동일한 엔티티 수로 읽었다. 실행 예:
설정 창과 같은 JSON 계획으로 세 레이어의 CAD 이름을 각각 `CAD-0-연속지적도`,
`CAD-0-지적도근점`, `CAD-0-건물`로 바꾸어 내보낸 결과도 GDAL이 각 레이어의
엔티티 수 418,442 / 23,942 / 328,176을 그대로 읽었다. 즉 이름 변경 옵션이
프로젝트의 다른 레이어를 누락시키지 않는 것은 자동 검증되었다.

```sh
GOGIS_TEST_DXF_WORKSPACE=/path/to/project.gogis \
  CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^TestDesktopDXFIntegrationWorkspace$' -count=1 -v
```

GUI에서 DXF 내보내기를 누르면 먼저 레이어 포함 여부, CAD 레이어 이름,
도형/레이블 내보내기 여부와 문자 인코딩을 선택한다. 선택한 이름이 중복되거나
내보낼 레이어가 없으면 파일 저장 단계로 진행하지 않는다.

1. 새 빌드에서 문제의 원본 레이어를 다시 DXF로 내보내고, 지원되는 OS의 ARES
   Commander 2027에서 연다. 이전 `test.dxf`가 아니라 새 파일을 확인한다.
   세종시 예제의 초기 중심은 대략 X 224,106, Y 441,354여야 한다.
2. 전체 범위에서 시작해 도면을 이동하고 축소/확대하여, 특히 건물 레이어의 채움이
   처음 화면에 나타나는 구간을 통과한다. 복구 경고, 응답 없음, 충돌 없이 건물 채움과
   외곽선이 표시되는지 확인한다. 이 동작이 SOLID 대체와 초기 VPORT의 필수 실테스트다.
3. `testdata/ares/sample-project-utf8.dxf`를 먼저 열어 세 CAD 레이어와 도형,
   `도근점 1` TEXT, 건물 SOLID 채움과 구멍, 원형 십자 POINT 심볼이 표시되는지 확인한다.
   이어 CP949 샘플도 확인한다.
4. `testdata/ares/sample-utf8.dxf`를 연다.
5. 파일을 열 때 오류·복구 대화상자가 없는지 확인한다.
6. `sample_labeled` layer의 선과 폴리곤 위치가 원본 좌표와 일치하는지 확인한다.
7. geometry와 함께 `한글 도로`, `한글 건물` 레이블이 깨지지 않는지 확인한다.
8. 도로 레이블의 30° 회전과 두 레이블의 위치, 높이(2.5), `Korean` text style을 확인한다.
9. `testdata/ares/sample-cp949.dxf`도 열고 동일한 항목을 확인한다.
10. ARES에서 저장한 파일을 다시 열어 entity와 한글이 유지되는지 확인한다.

검증 기록에는 ARES 버전, 운영체제 버전, 사용한 profile, 파일 SHA-256,
결과(성공/실패), 오류 메시지와 화면 캡처 경로를 함께 남긴다. 실제 ARES
검증 결과가 추가되기 전까지 `ares-utf8`과 `ares-cp949`는 모두 실험적
profile이며 어느 한쪽을 호환성 정답으로 간주하지 않는다.
