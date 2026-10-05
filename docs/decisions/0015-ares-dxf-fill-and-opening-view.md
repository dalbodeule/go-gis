# ARES용 DXF 채움 및 초기 시점

## 결정

Native 데스크톱의 DXF 내보내기는 채워진 폴리곤을 GEOS의 constrained Delaunay
triangulation으로 나눈 뒤 DXF `SOLID` 삼각형으로 기록한다. 구멍은 삼각분할에서
제외하고, 원본 폴리곤 외곽선과 내부 경계는 `LWPOLYLINE`으로 계속 기록한다.
삼각분할이 실패하면 이전 파일을 유지하고 내보내기 오류를 표시한다. 독립적으로
사용하는 범용 DXF exporter에는 삼각분할기가 없을 때의 HATCH 경로를 남긴다.

초기 화면은 `$VIEWCTR`/`$VIEWSIZE`와 함께 VPORT 테이블의 `*ACTIVE` 레코드에도
같은 중심과 높이를 기록한다. 선·면 레이어가 있으면 점 레이어의 좌표 이상치는
초기 시점 계산에서 제외한다. 실제 좌표와 레이어 데이터는 변경하지 않는다.

## 근거와 영향

Windows ARES Commander에서 HATCH가 포함된 도면은 경계 플래그를 수정한 뒤에도
건물이 나타나는 순간 렌더링이 멈췄다. 같은 도면의 채움을 끄면 멈춤이 사라졌다.
`SOLID`는 DXF의 채워진 삼각/사각형 엔티티이고, GEOS 삼각분할은 내부 구멍을
보존한다. 다만 삼각형마다 엔티티 하나가 필요하므로 파일 크기와 엔티티 수가
늘어난다. 실제 3-레이어 작업공간에서는 건물 77,352개로부터 `SOLID` 250,816개와
건물 경계 `LWPOLYLINE` 77,360개를 기록했고 GDAL 재읽기가 통과했다. ARES의
실제 렌더링 성능은 Windows에서 재검증해야 한다.

실제 지적도근점 레이어의 범위에는 X 2,287,874.9와 Y 43,257.02의 이상치가
있다. 건물·연속지적도 범위는 대략 X 211,407–236,806, Y 423,224–459,485다.
헤더의 초기 중심만으로는 ARES가 원점에 가까운 시점으로 열렸으므로, 현대 DXF
리더가 우선 사용하는 `*ACTIVE` 뷰포트를 추가했다.

명세: [Autodesk SOLID](https://help.autodesk.com/cloudhelp/2018/ENU/AutoCAD-DXF/files/GUID-E0C5F04E-D0C5-48F5-AC09-32733E8848F2.htm),
[Autodesk VPORT](https://help.autodesk.com/cloudhelp/2023/ENU/AutoCAD-DXF/files/GUID-8CE7CC87-27BD-4490-89DA-C21F516415A9.htm),
[VPORT와 헤더 변수의 우선순위](https://help.autodesk.com/cloudhelp/2024/ENU/AutoCAD-DXF/files/GUID-ED26E626-BC45-4256-9914-87E5FFE934B8.htm),
[GEOS constrained Delaunay triangulation](https://libgeos.org/doxygen/geos__c_8h.html).
