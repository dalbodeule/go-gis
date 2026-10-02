import QtQuick
import QtQuick.Window
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs as QuickDialogs
import Qt.labs.platform as Platform
import GoGIS 1.0

ApplicationWindow {
    id: rootWindow
    visible: true
    width: 1440
    height: 900
    minimumWidth: 960
    minimumHeight: 640
    title: "GoGIS — Milestone B prototype"
    color: "#f4f6f8"
    property bool vertexEditMode: false
    property string language: typeof appLanguage === "undefined" ? "en" : appLanguage
    property string versionText: typeof appVersion === "undefined" ? "0.1.0-dev" : appVersion
    property string runtimeText: typeof appRuntime === "undefined" ? "Go runtime" : appRuntime
    property string buildTargetText: typeof appBuildTarget === "undefined" ? "desktop" : appBuildTarget
    property var translations: ({
        en: ({"Add vector files": "Add vector files", "Open workspace": "Open workspace", "Attributes": "Attributes", "Edit vertices": "Edit vertices", "Finish vertex edit": "Finish vertex edit", "Save GeoPackage": "Save GeoPackage", "Save workspace": "Save workspace", "About GoGIS": "About GoGIS", "Layers": "Layers", "No layers yet": "No layers yet", "Add vector files or open a workspace to begin.": "Add vector files or open a workspace to begin.", "Drag to pan · Scroll to zoom · Click a feature to inspect": "Drag to pan · Scroll to zoom · Click a feature to inspect", "General": "General", "Data source": "Data source", "Symbology": "Symbology", "Labels and expressions": "Labels and expressions", "Layer properties": "Layer properties", "Version": "Version", "Build": "Build", "Runtime": "Runtime", "License": "License", "Close": "Close", "Layer": "Layer", "Layer visible": "Layer visible", "Browse…": "Browse…", "Layer in source": "Layer in source", "Shapefile encoding": "Shapefile encoding", "Point color": "Point color", "Point size (mm)": "Point size (mm)", "Line color": "Line color", "Line width (mm)": "Line width (mm)", "Polygon color": "Polygon color", "Fill opacity (0–1)": "Fill opacity (0–1)", "Show labels": "Show labels", "Label field / template": "Label field / template", "Placement": "Placement", "Rotation field (optional)": "Rotation field (optional)", "Text height (mm)": "Text height (mm)", "Minimum scale denominator": "Minimum scale denominator", "Maximum scale denominator": "Maximum scale denominator", "Display rule — return true to show this feature's label": "Display rule — return true to show this feature's label", "Label text — return string, number, or nil": "Label text — return string, number, or nil", "Insert label example": "Insert label example", "Insert rule example": "Insert rule example", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.", "Layer settings": "Layer settings", "Source path": "Source path", "Data properties": "Data properties"}),
        ko: ({"Add vector files": "벡터 파일 추가", "Open workspace": "작업공간 열기", "Attributes": "속성 테이블", "Edit vertices": "정점 편집", "Finish vertex edit": "정점 편집 종료", "Save GeoPackage": "GeoPackage 저장", "Save workspace": "작업공간 저장", "About GoGIS": "GoGIS 정보", "Layers": "레이어", "No layers yet": "레이어가 없습니다", "Add vector files or open a workspace to begin.": "벡터 파일을 추가하거나 작업공간을 열어 시작하세요.", "Drag to pan · Scroll to zoom · Click a feature to inspect": "드래그: 이동 · 휠: 확대/축소 · 피처 클릭: 정보 확인", "General": "일반", "Data source": "데이터 원본", "Symbology": "심볼로지", "Labels and expressions": "레이블 및 표현식", "Layer properties": "레이어 속성", "Version": "버전", "Build": "빌드", "Runtime": "실행 환경", "License": "라이선스", "Close": "닫기", "Layer": "레이어", "Layer visible": "레이어 표시", "Browse…": "찾아보기…", "Layer in source": "원본 내부 레이어", "Shapefile encoding": "Shapefile 인코딩", "Point color": "점 색상", "Point size (mm)": "점 크기 (mm)", "Line color": "선 색상", "Line width (mm)": "선 두께 (mm)", "Polygon color": "폴리곤 색상", "Fill opacity (0–1)": "채우기 불투명도 (0–1)", "Show labels": "레이블 표시", "Label field / template": "레이블 필드 / 템플릿", "Placement": "배치", "Rotation field (optional)": "회전 필드 (선택)", "Text height (mm)": "글자 높이 (mm)", "Minimum scale denominator": "최소 축척 분모", "Maximum scale denominator": "최대 축척 분모", "Display rule — return true to show this feature's label": "표시 규칙 — 레이블 표시 시 true 반환", "Label text — return string, number, or nil": "레이블 문자열 — 문자열, 숫자 또는 nil 반환", "Insert label example": "레이블 예제 삽입", "Insert rule example": "규칙 예제 삽입", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "스크립트는 피처마다 실행됩니다. 읽기 전용 `feature` 테이블로 속성에 접근합니다. 공백이 있는 필드는 feature[\"필드 이름\"] 형식을 사용하세요.", "Layer settings": "레이어 설정", "Source path": "원본 경로", "Data properties": "데이터 속성"}),
        jp: ({"Add vector files": "ベクターファイルを追加", "Open workspace": "ワークスペースを開く", "Attributes": "属性テーブル", "Edit vertices": "頂点を編集", "Finish vertex edit": "頂点編集を終了", "Save GeoPackage": "GeoPackageを保存", "Save workspace": "ワークスペースを保存", "About GoGIS": "GoGISについて", "Layers": "レイヤー", "No layers yet": "レイヤーがありません", "Add vector files or open a workspace to begin.": "ベクターファイルを追加するか、ワークスペースを開いてください。", "Drag to pan · Scroll to zoom · Click a feature to inspect": "ドラッグ: 移動 · ホイール: 拡大/縮小 · 地物をクリック: 情報表示", "General": "一般", "Data source": "データソース", "Symbology": "シンボロジ", "Labels and expressions": "ラベルと式", "Layer properties": "レイヤーのプロパティ", "Version": "バージョン", "Build": "ビルド", "Runtime": "ランタイム", "License": "ライセンス", "Close": "閉じる", "Layer": "レイヤー", "Layer visible": "レイヤーを表示", "Browse…": "参照…", "Layer in source": "ソース内レイヤー", "Shapefile encoding": "Shapefileの文字コード", "Point color": "ポイント色", "Point size (mm)": "ポイントサイズ (mm)", "Line color": "ライン色", "Line width (mm)": "ライン幅 (mm)", "Polygon color": "ポリゴン色", "Fill opacity (0–1)": "塗りの不透明度 (0–1)", "Show labels": "ラベルを表示", "Label field / template": "ラベルフィールド / テンプレート", "Placement": "配置", "Rotation field (optional)": "回転フィールド (任意)", "Text height (mm)": "文字の高さ (mm)", "Minimum scale denominator": "最小縮尺分母", "Maximum scale denominator": "最大縮尺分母", "Display rule — return true to show this feature's label": "表示ルール — ラベル表示時にtrueを返す", "Label text — return string, number, or nil": "ラベル文字列 — 文字列、数値、またはnilを返す", "Insert label example": "ラベル例を挿入", "Insert rule example": "ルール例を挿入", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "スクリプトは地物ごとに実行されます。読み取り専用の`feature`テーブルから属性を参照できます。空白を含むフィールド名はfeature[\"フィールド名\"]を使用します。", "Layer settings": "レイヤー設定", "Source path": "ソースパス", "Data properties": "データ属性"})
    })
    property var translationOverrides: ({
        en: ({"Desktop GIS": "Desktop GIS", "Layer properties": "Layer properties", "Select original layer source": "Select original layer source", "Add vector files as layers": "Add vector files as layers", "Save GoGIS workspace": "Save GoGIS workspace", "Open GoGIS workspace": "Open GoGIS workspace", "Drop SHP, GeoPackage, or GeoJSON": "Drop SHP, GeoPackage, or GeoJSON", "Display name": "Display name", "Original source path": "Original source path", "Internal layer name": "Internal layer name", "Auto encoding": "Auto encoding", "Selected feature": "Selected feature", "Feature name": "Feature name", "Save": "Save", "Cancel": "Cancel", "Previous": "Previous", "Next": "Next", "No attribute records in this layer": "No attribute records in this layer", "X coordinate": "X coordinate", "Y coordinate": "Y coordinate", "Go": "Go", "Cancel loading/render": "Cancel loading/render", "Layer settings": "Layer settings", "Source path": "Source path", "Data properties": "Data properties", "Point color": "Point color", "Point size (mm)": "Point size (mm)", "Line color": "Line color", "Line width (mm)": "Line width (mm)", "Polygon color": "Polygon color", "Fill opacity (0–1)": "Fill opacity (0–1)", "Show labels": "Show labels", "Label field / template": "Label field / template", "Placement": "Placement", "Rotation field (optional)": "Rotation field (optional)", "Text height (mm)": "Text height (mm)", "Minimum scale denominator": "Minimum scale denominator", "Maximum scale denominator": "Maximum scale denominator"}),
        ko: ({"Desktop GIS": "데스크톱 GIS", "Layer properties": "레이어 속성", "Select original layer source": "레이어 원본 선택", "Add vector files as layers": "벡터 파일을 레이어로 추가", "Save GoGIS workspace": "GoGIS 작업공간 저장", "Open GoGIS workspace": "GoGIS 작업공간 열기", "Drop SHP, GeoPackage, or GeoJSON": "SHP, GeoPackage 또는 GeoJSON 파일을 놓으세요", "Display name": "표시 이름", "Original source path": "원본 경로", "Internal layer name": "내부 레이어 이름", "Auto encoding": "인코딩 자동 감지", "Selected feature": "선택한 피처", "Feature name": "피처 이름", "Save": "저장", "Cancel": "취소", "Previous": "이전", "Next": "다음", "No attribute records in this layer": "이 레이어에 속성 레코드가 없습니다", "X coordinate": "X 좌표", "Y coordinate": "Y 좌표", "Go": "이동", "Cancel loading/render": "불러오기/렌더링 취소", "Layer settings": "레이어 설정", "Source path": "원본 경로", "Data properties": "데이터 속성", "Point color": "점 색상", "Point size (mm)": "점 크기 (mm)", "Line color": "선 색상", "Line width (mm)": "선 두께 (mm)", "Polygon color": "폴리곤 색상", "Fill opacity (0–1)": "채우기 불투명도 (0–1)", "Show labels": "레이블 표시", "Label field / template": "레이블 필드 / 템플릿", "Placement": "배치", "Rotation field (optional)": "회전 필드 (선택)", "Text height (mm)": "글자 높이 (mm)", "Minimum scale denominator": "최소 축척 분모", "Maximum scale denominator": "최대 축척 분모"}),
        jp: ({"Desktop GIS": "デスクトップGIS", "Layer properties": "レイヤーのプロパティ", "Select original layer source": "レイヤーソースを選択", "Add vector files as layers": "ベクターファイルをレイヤーとして追加", "Save GoGIS workspace": "GoGISワークスペースを保存", "Open GoGIS workspace": "GoGISワークスペースを開く", "Drop SHP, GeoPackage, or GeoJSON": "SHP、GeoPackage、GeoJSONをドロップ", "Display name": "表示名", "Original source path": "元のソースパス", "Internal layer name": "内部レイヤー名", "Auto encoding": "文字コードを自動判定", "Selected feature": "選択地物", "Feature name": "地物名", "Save": "保存", "Cancel": "キャンセル", "Previous": "前へ", "Next": "次へ", "No attribute records in this layer": "このレイヤーに属性レコードはありません", "X coordinate": "X座標", "Y coordinate": "Y座標", "Go": "移動", "Cancel loading/render": "読み込み/描画をキャンセル", "Layer settings": "レイヤー設定", "Source path": "ソースパス", "Data properties": "データ属性", "Point color": "ポイント色", "Point size (mm)": "ポイントサイズ (mm)", "Line color": "ライン色", "Line width (mm)": "ライン幅 (mm)", "Polygon color": "ポリゴン色", "Fill opacity (0–1)": "塗りの不透明度 (0–1)", "Show labels": "ラベルを表示", "Label field / template": "ラベルフィールド / テンプレート", "Placement": "配置", "Rotation field (optional)": "回転フィールド (任意)", "Text height (mm)": "文字の高さ (mm)", "Minimum scale denominator": "最小縮尺分母", "Maximum scale denominator": "最大縮尺分母"})
    })

    property var luaTranslations: ({
        en: ({
            "Lua label editor": "Lua label editor",
            "Open Lua editor and examples…": "Open Lua editor and examples…",
            "Lua field access hint": "feature.FIELD reads an attribute; use feature[\"field name\"] when a field contains spaces. Types: %1",
            "Lua API help": "Lua API: gogis.layers(); gogis.filter_lua(source, result, predicate); gogis.label_lua(source, result, text, rule, height, style). Return boolean from rules and string/number/nil from label text.",
            "Insert field into rule…": "Insert field into rule…",
            "Insert field into label…": "Insert field into label…",
            "Center": "Center", "Center + rotation": "Center + rotation", "Free angle": "Free angle",
            "Lua field types": "Lua field types", "Open the attribute table to inspect the layer schema.": "Open the attribute table to inspect the layer schema."
        }),
        ko: ({
            "Lua label editor": "Lua 레이블 편집기",
            "Open Lua editor and examples…": "Lua 편집기 및 예제 열기…",
            "Lua field access hint": "feature.FIELD로 속성을 읽습니다. 필드명에 공백이 있으면 feature[\"필드 이름\"]을 사용하세요. 필드 형식: %1",
            "Lua API help": "Lua API: gogis.layers(); gogis.filter_lua(source, result, predicate); gogis.label_lua(source, result, text, rule, height, style). 규칙은 boolean, 레이블 식은 string/number/nil을 반환합니다.",
            "Insert field into rule…": "규칙에 필드 삽입…",
            "Insert field into label…": "레이블에 필드 삽입…",
            "Center": "중앙", "Center + rotation": "중앙 + 회전", "Free angle": "자유 각도",
            "Lua field types": "Lua 필드 형식", "Open the attribute table to inspect the layer schema.": "레이어 필드를 확인하려면 속성 테이블을 여세요."
        }),
        jp: ({
            "Lua label editor": "Luaラベルエディター",
            "Open Lua editor and examples…": "Luaエディターと例を開く…",
            "Lua field access hint": "feature.FIELDで属性を読み取ります。空白を含むフィールド名にはfeature[\"フィールド名\"]を使用します。型: %1",
            "Lua API help": "Lua API: gogis.layers(); gogis.filter_lua(source, result, predicate); gogis.label_lua(source, result, text, rule, height, style)。ルールはboolean、ラベル式はstring/number/nilを返します。",
            "Insert field into rule…": "ルールにフィールドを挿入…",
            "Insert field into label…": "ラベルにフィールドを挿入…",
            "Center": "中央", "Center + rotation": "中央 + 回転", "Free angle": "自由角度",
            "Lua field types": "Luaフィールド型", "Open the attribute table to inspect the layer schema.": "レイヤーの項目を確認するには属性テーブルを開いてください。"
        })
    })

    property var diagnosticTranslations: ({
        en: ({"Logs": "Logs", "Application log": "Application log", "Showing the most recent process output and application errors. Older entries are discarded.": "Showing the most recent process output and application errors. Older entries are discarded."}),
        ko: ({"Logs": "로그", "Application log": "애플리케이션 로그", "Showing the most recent process output and application errors. Older entries are discarded.": "최근 프로세스 출력과 애플리케이션 오류를 표시합니다. 오래된 항목은 순차적으로 삭제됩니다."}),
        jp: ({"Logs": "ログ", "Application log": "アプリケーションログ", "Showing the most recent process output and application errors. Older entries are discarded.": "最近のプロセス出力とアプリケーションエラーを表示します。古い項目は順次削除されます。"})
    })

    property var viewTranslations: ({
        en: ({"Zoom to full extent": "Zoom to full extent"}),
        ko: ({"Zoom to full extent": "전체 범위", "Remove layer…": "레이어 제거…", "Remove layer from project": "프로젝트에서 레이어 제거", "Remove '%1' from this project? The source file will not be deleted.": "이 프로젝트에서 '%1' 레이어를 제거할까요? 원본 파일은 삭제되지 않습니다.", "Transparent": "투명", "Opaque": "불투명", "Set point symbols, boundary lines, and polygon fill. Colors use #RRGGBB; sizes are in millimeters on screen.": "점·경계선·폴리곤 채우기를 설정합니다. 색상은 #RRGGBB, 크기는 화면 기준 mm입니다.", "Outline only (no polygon fill)": "윤곽선만 표시 (채우기 없음)", "Fill opacity: 0 = transparent, 1 = opaque. Boundary lines remain visible. This setting affects polygons only.": "채우기 불투명도: 0은 투명, 1은 불투명입니다. 경계선은 계속 보이며 폴리곤에만 적용됩니다.", "Available fields…": "사용 가능한 필드…", "Choose a field or type a template, for example ${NAME} (${CODE}).": "필드를 선택하거나 ${NAME} (${CODE})처럼 템플릿을 입력하세요.", "Fields for this layer": "이 레이어의 필드", "Click a field to insert ${FIELD} at the cursor. A single field name also works.": "필드를 누르면 커서 위치에 ${FIELD}가 삽입됩니다. 필드명만 입력해도 됩니다.", "Field list is loading or unavailable for this layer.": "필드 목록을 불러오는 중이거나 이 레이어에서는 사용할 수 없습니다."}),
        jp: ({"Zoom to full extent": "全体表示", "Remove layer…": "レイヤーを除去…", "Remove layer from project": "プロジェクトからレイヤーを除去", "Remove '%1' from this project? The source file will not be deleted.": "このプロジェクトから'%1'を除去しますか？元のファイルは削除しません。", "Transparent": "透明", "Opaque": "不透明", "Set point symbols, boundary lines, and polygon fill. Colors use #RRGGBB; sizes are in millimeters on screen.": "点・境界線・ポリゴンの塗りを設定します。色は#RRGGBB、サイズは画面上のmmです。", "Outline only (no polygon fill)": "輪郭線のみ表示（塗りなし）", "Fill opacity: 0 = transparent, 1 = opaque. Boundary lines remain visible. This setting affects polygons only.": "塗りの不透明度: 0は透明、1は不透明です。境界線は表示されたままです。ポリゴンのみに適用されます。", "Available fields…": "使用可能なフィールド…", "Choose a field or type a template, for example ${NAME} (${CODE}).": "フィールドを選ぶか、${NAME} (${CODE})のように入力してください。", "Fields for this layer": "このレイヤーのフィールド", "Click a field to insert ${FIELD} at the cursor. A single field name also works.": "フィールドを選ぶとカーソル位置に${FIELD}を挿入します。フィールド名だけでも使えます。", "Field list is loading or unavailable for this layer.": "フィールド一覧を読み込み中、またはこのレイヤーでは利用できません。"})
    })

    function tr(key) {
        var override = translationOverrides[language] || translationOverrides.en;
        if (override[key] !== undefined)
            return override[key];
        var luaDictionary = luaTranslations[language] || luaTranslations.en;
        if (luaDictionary[key] !== undefined)
            return luaDictionary[key];
        var diagnosticDictionary = diagnosticTranslations[language] || diagnosticTranslations.en;
        if (diagnosticDictionary[key] !== undefined)
            return diagnosticDictionary[key];
        var viewDictionary = viewTranslations[language] || viewTranslations.en;
        if (viewDictionary[key] !== undefined)
            return viewDictionary[key];
        var dictionary = translations[language] || translations.en;
        return dictionary[key] || translations.en[key] || key;
    }

    function localizedStatus(raw) {
        var status = String(raw || "");
        if (status === "Removing layer")
            return language === "ko" ? "레이어 제거 중" : language === "jp" ? "レイヤーを除去中" : status;
        if (status.indexOf("Layer removed: ") === 0)
            return (language === "ko" ? "레이어 제거 완료: " : language === "jp" ? "レイヤーを除去しました: " : "Layer removed: ") + status.substring("Layer removed: ".length);
        if (status.indexOf("Remove layer failed: ") === 0)
            return (language === "ko" ? "레이어 제거 실패: " : language === "jp" ? "レイヤーの除去に失敗しました: " : "Remove layer failed: ") + status.substring("Remove layer failed: ".length);
        var dictionary = {
            en: {"Loading": "Loading", "Loading: checking feature count": "Checking feature count", "Preview displayed; loading full data": "Preview displayed; loading full data", "Layer settings applied": "Layer settings applied", "Loading cancelled": "Loading cancelled", "Render cancelled": "Render cancelled", "Dataset size unavailable; opened read-only to limit memory": "Dataset size unavailable; opened read-only to limit memory", "Render incomplete; zoom in and try again: ": "Render incomplete; zoom in and try again: ", "Open failed: ": "Open failed: ", "Save failed: ": "Save failed: ", "Workspace load failed: ": "Workspace load failed: ", "Layer settings failed: ": "Layer settings failed: ", "Render stopped: ": "Render stopped: ", "Render error: ": "Render error: ", "Render skipped: ": "Render skipped: ", "Loading ": "Loading ", "Saving ": "Saving ", "Saved ": "Saved ", "Dataset has at least ": "Dataset has at least "},
            ko: {"Loading": "불러오는 중", "Loading: checking feature count": "피처 개수 확인 중", "Preview displayed; loading full data": "미리보기 표시됨 · 전체 데이터 불러오는 중", "Layer settings applied": "레이어 설정 적용 완료", "Loading cancelled": "불러오기 취소됨", "Render cancelled": "렌더링 취소됨", "Dataset size unavailable; opened read-only to limit memory": "데이터 크기를 알 수 없어 메모리 보호를 위해 읽기 전용으로 열었습니다", "Render incomplete; zoom in and try again: ": "일부 렌더링을 완료하지 못했습니다. 확대 후 다시 시도하세요: ", "Open failed: ": "열기 실패: ", "Save failed: ": "저장 실패: ", "Workspace load failed: ": "작업공간 열기 실패: ", "Workspace save failed: ": "작업공간 저장 실패: ", "Layer settings failed: ": "레이어 설정 실패: ", "Layer reload failed: ": "레이어 다시 열기 실패: ", "Attribute page failed: ": "속성 페이지 표시 실패: ", "Attribute page exceeds the 16 MiB display payload limit": "속성 페이지가 16 MiB 표시 한도를 초과했습니다", "Render stopped: ": "렌더링 중단: ", "Render error: ": "렌더링 오류: ", "Render skipped: ": "렌더링 생략: ", "Label display skipped: ": "레이블 표시 생략: ", "Loading ": "불러오는 중: ", "Saving ": "저장 중: ", "Saved ": "저장 완료: ", "Workspace saved ": "작업공간 저장 완료: ", "Workspace loaded ": "작업공간 열기 완료: ", "Loading workspace ": "작업공간 불러오는 중: ", "Add files requires the native GDAL build": "파일을 추가하려면 GDAL 지원 데스크톱 빌드가 필요합니다", "Reopening layer source with selected encoding": "선택한 인코딩으로 레이어 원본 다시 여는 중", "Layer source reloaded": "레이어 원본 다시 열기 완료", "Vertex edit failed: ": "정점 편집 실패: ", "Vertex moved": "정점 이동 완료", "No visible layers": "표시 중인 레이어가 없습니다", "No vector files selected": "벡터 파일을 선택하지 않았습니다", "A file load is already in progress": "파일을 불러오는 중입니다", "Selected source is already loaded": "선택한 원본이 이미 열려 있습니다", "Cannot add files until the read-only source is ready": "읽기 전용 원본이 준비될 때까지 파일을 추가할 수 없습니다", "Open cancelled": "열기를 취소했습니다", "Save cancelled": "저장을 취소했습니다", "Dataset has at least ": "피처가 최소 "},
            jp: {"Loading": "読み込み中", "Loading: checking feature count": "地物数を確認中", "Preview displayed; loading full data": "プレビューを表示 · 全データを読み込み中", "Layer settings applied": "レイヤー設定を適用しました", "Loading cancelled": "読み込みをキャンセルしました", "Render cancelled": "描画をキャンセルしました", "Dataset size unavailable; opened read-only to limit memory": "メモリ保護のため読み取り専用で開きました（データ件数不明）", "Render incomplete; zoom in and try again: ": "一部を描画できません。拡大して再試行してください: ", "Open failed: ": "開けませんでした: ", "Save failed: ": "保存できませんでした: ", "Workspace load failed: ": "ワークスペースを開けませんでした: ", "Workspace save failed: ": "ワークスペースを保存できませんでした: ", "Layer settings failed: ": "レイヤー設定に失敗しました: ", "Layer reload failed: ": "レイヤーを再読み込みできませんでした: ", "Attribute page failed: ": "属性ページを表示できませんでした: ", "Attribute page exceeds the 16 MiB display payload limit": "属性ページが16 MiBの表示上限を超えました", "Render stopped: ": "描画を停止しました: ", "Render error: ": "描画エラー: ", "Render skipped: ": "描画を省略しました: ", "Label display skipped: ": "ラベル表示を省略しました: ", "Loading ": "読み込み中: ", "Saving ": "保存中: ", "Saved ": "保存しました: ", "Workspace saved ": "ワークスペースを保存しました: ", "Workspace loaded ": "ワークスペースを開きました: ", "Workspace loaded; relink unavailable layers: ": "ワークスペースを開きました。再リンクが必要なレイヤー: ", "Loading workspace ": "ワークスペースを読み込み中: ", "Add files requires the native GDAL build": "ファイル追加にはGDAL対応デスクトップビルドが必要です", "Reopening layer source with selected encoding": "選択した文字コードでレイヤーソースを再読み込み中", "Layer source reloaded": "レイヤーソースを再読み込みしました", "Vertex edit failed: ": "頂点編集に失敗しました: ", "Vertex moved": "頂点を移動しました", "No visible layers": "表示中のレイヤーがありません", "No vector files selected": "ベクターファイルが選択されていません", "A file load is already in progress": "ファイルを読み込み中です", "Selected source is already loaded": "選択したソースは既に開いています", "Cannot add files until the read-only source is ready": "読み取り専用ソースの準備完了後に追加できます", "Open cancelled": "開く操作をキャンセルしました", "Save cancelled": "保存をキャンセルしました", "Dataset has at least ": "地物数が少なくとも "}
        }[language] || {};
        if (status.indexOf("Dataset has at least ") === 0) {
            var count = status.substring("Dataset has at least ".length).split(" features;")[0];
            if (language === "ko") return "피처 " + count + "개 이상 · 메모리 보호를 위해 읽기 전용으로 열었습니다";
            if (language === "jp") return "地物 " + count + " 件以上 · メモリ保護のため読み取り専用で開きました";
            return status;
        }
        var loadingFiles = /^Loading (\d+) vector file\(s\)$/.exec(status);
        if (loadingFiles) {
            if (language === "ko") return "벡터 파일 " + loadingFiles[1] + "개 불러오는 중";
            if (language === "jp") return "ベクターファイル " + loadingFiles[1] + " 件を読み込み中";
            return status;
        }
        var queuedSelections = /^Loading in progress; (\d+) selection\(s\) queued$/.exec(status);
        if (queuedSelections) {
            if (language === "ko") return "파일 불러오는 중 · 추가 요청 " + queuedSelections[1] + "건 대기";
            if (language === "jp") return "ファイル読み込み中 · 追加要求 " + queuedSelections[1] + " 件待機";
            return status;
        }
        var exact = dictionary[status];
        if (exact !== undefined) return exact;
        if (status.indexOf("Workspace loaded; relink unavailable layers: ") === 0 && language === "ko") return "작업공간을 열었습니다. 다시 연결할 레이어: " + status.substring("Workspace loaded; relink unavailable layers: ".length);
        var prefixes = Object.keys(dictionary).sort(function(a, b) { return b.length - a.length; });
        for (var i = 0; i < prefixes.length; ++i) {
            if (status.indexOf(prefixes[i]) === 0) return dictionary[prefixes[i]] + status.substring(prefixes[i].length);
        }
        return status;
    }

    function statusColor(raw) {
        var status = String(raw || "").toLowerCase();
        if (status.indexOf("failed") >= 0 || status.indexOf("error") >= 0 || status.indexOf("invalid") >= 0 || status.indexOf("exceeds") >= 0 || status.indexOf("incomplete") >= 0 || status.indexOf("stopped") >= 0 || status.indexOf("requires") >= 0) return "#b42318";
        if (status.indexOf("cancel") >= 0 || status.indexOf("unavailable") >= 0 || status.indexOf("cannot") >= 0 || status.indexOf("skipped") >= 0 || status.indexOf("relink") >= 0) return "#9a6700";
        if (status.indexOf("read-only") >= 0 || status.indexOf("readonly") >= 0 || status.indexOf("loading") >= 0 || status.indexOf("saving") >= 0 || status.indexOf("removing") >= 0 || status.indexOf("preview") >= 0 || status.indexOf("checking") >= 0) return "#175cd3";
        if (status.indexOf("saved") >= 0 || status.indexOf("applied") >= 0 || status.indexOf("loaded") >= 0 || status.indexOf("removed") >= 0 || status.indexOf("ready") >= 0) return "#2e7d32";
        return "#65717d";
    }

    function statusIsBusy(raw) {
        var status = String(raw || "").toLowerCase();
        return status.indexOf("loading") >= 0 || status.indexOf("saving") >= 0 || status.indexOf("rendering") >= 0 || status.indexOf("preview") >= 0 || status.indexOf("removing") >= 0;
    }

    function memorySummary(processBytes, heapBytes, available, kind) {
        function mib(bytes) { return (Number(bytes) / (1024 * 1024)).toFixed(0); }
        var metric = kind === 2 ? (language === "ko" ? "최고 RSS" : language === "jp" ? "最大RSS" : "peak RSS") : kind === 1 ? (language === "ko" ? "작업 집합" : language === "jp" ? "ワーキングセット" : "working set") : "RSS";
        var processLabel = language === "ko" ? "프로세스 " + metric : language === "jp" ? "プロセス" + metric : "Process " + metric;
        var heapLabel = language === "ko" ? "Go 힙" : language === "jp" ? "Goヒープ" : "Go heap";
        if (!available) return heapLabel + " " + mib(heapBytes) + " MiB · " + (language === "ko" ? "프로세스 메모리 측정 불가" : language === "jp" ? "プロセスメモリ測定不可" : "process memory unavailable");
        return processLabel + " " + mib(processBytes) + " MiB · " + heapLabel + " " + mib(heapBytes) + " MiB";
    }

    component LuaCodeEditor: Item {
        id: luaCodeEditor
        property alias text: sourceArea.text
        property alias placeholderText: sourceArea.placeholderText
        property alias cursorPosition: sourceArea.cursorPosition
        property string highlightedHTML: ""
        property string highlightedLineNumbers: "1"
        readonly property var keywordLookup: ({"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true, "false": true, "for": true, "function": true, "if": true, "in": true, "local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true, "then": true, "true": true, "until": true, "while": true})
        readonly property var apiLookup: ({"feature": true, "gogis": true, "math": true, "string": true, "table": true, "ipairs": true, "pairs": true, "tonumber": true, "tostring": true, "type": true})
        implicitHeight: 144
        clip: true

        function scheduleHighlight() {
            highlightTimer.restart();
        }

        Timer {
            id: highlightTimer
            interval: 75
            repeat: false
            onTriggered: {
                luaCodeEditor.highlightedHTML = luaCodeEditor.highlightLua(sourceArea.text);
                luaCodeEditor.highlightedLineNumbers = luaCodeEditor.lineNumberText(sourceArea.text);
            }
        }

        Component.onCompleted: scheduleHighlight()

        Rectangle {
            anchors.fill: parent
            z: -1
            color: "#ffffff"
            border.color: sourceArea.activeFocus ? "#4682b4" : "#cfd6dd"
            radius: 3
        }

        function escapeHtml(text) {
            return text.replace(/[&<>\"]/g, escapeHtmlCharacter);
        }

        function escapeHtmlCharacter(character) {
            if (character === "&") return "&amp;";
            if (character === "<") return "&lt;";
            if (character === ">") return "&gt;";
            return "&quot;";
        }

        function lineNumberText(source) {
            var count = 1;
            for (var index = 0; index < source.length; ++index) {
                if (source[index] === "\n") ++count;
            }
            var lines = [];
            for (var line = 1; line <= count; ++line) lines.push(line);
            return lines.join("\n");
        }

        function insert(position, value) {
            sourceArea.insert(position, value);
        }

        function longBracketLevel(source, start) {
            if (source[start] !== "[") return -1;
            var cursor = start + 1;
            while (source[cursor] === "=") ++cursor;
            return source[cursor] === "[" ? cursor - start - 1 : -1;
        }

        function longBracketEnd(source, start, level) {
            var closing = "]";
            for (var equals = 0; equals < level; ++equals) closing += "=";
            closing += "]";
            var closeIndex = source.indexOf(closing, start);
            return closeIndex < 0 ? source.length : closeIndex + closing.length;
        }

        function highlightLua(source) {
            var output = ["<pre style='margin:0'>"];
            var index = 0;
            while (index < source.length) {
                var start = index;
                var character = source[index];
                var category = "";
                if (character === "-" && source[index + 1] === "-") {
                    category = "comment";
                    var commentLevel = longBracketLevel(source, index + 2);
                    if (commentLevel >= 0)
                        index = longBracketEnd(source, index + 2 + commentLevel + 2, commentLevel);
                    else
                        while (index < source.length && source[index] !== "\n") index++;
                } else if (character === "[") {
                    var stringLevel = longBracketLevel(source, index);
                    if (stringLevel >= 0) {
                        category = "string";
                        index = longBracketEnd(source, index + stringLevel + 2, stringLevel);
                    } else {
                        index++;
                    }
                } else if (character === "\"" || character === "'") {
                    category = "string";
                    var quote = character;
                    index++;
                    while (index < source.length) {
                        if (source[index] === "\\") { index += 2; continue; }
                        if (source[index++] === quote) break;
                    }
                } else if (/[A-Za-z_]/.test(character)) {
                    index++;
                    while (index < source.length && /[A-Za-z0-9_]/.test(source[index])) index++;
                    var identifier = source.slice(start, index);
                    if (keywordLookup[identifier]) category = "keyword";
                    else if (apiLookup[identifier]) category = "api";
                } else if (/[0-9]/.test(character)) {
                    category = "number";
                    index++;
                    while (index < source.length && /[0-9.eE+-]/.test(source[index])) index++;
                } else {
                    index++;
                }
                var token = source.slice(start, index);
                var escaped = escapeHtml(token);
                if (category === "keyword") output.push("<span style='color:#7b2cbf;font-weight:600'>", escaped, "</span>");
                else if (category === "string") output.push("<span style='color:#16803c'>", escaped, "</span>");
                else if (category === "number") output.push("<span style='color:#b45309'>", escaped, "</span>");
                else if (category === "comment") output.push("<span style='color:#78838e;font-style:italic'>", escaped, "</span>");
                else if (category === "api") output.push("<span style='color:#1769aa'>", escaped, "</span>");
                else output.push(escaped);
            }
            output.push("</pre>");
            return output.join("");
        }

        Text {
            id: lineNumbers
            x: 6
            y: sourceArea.topPadding - sourceArea.contentY
            width: 30
            text: luaCodeEditor.highlightedLineNumbers
            horizontalAlignment: Text.AlignRight
            color: "#8a949e"
            font: sourceArea.font
        }
        Rectangle {
            x: 43
            y: 0
            width: 1
            height: parent.height
            color: "#e2e6ea"
        }
        Text {
            id: syntaxText
            x: sourceArea.leftPadding - sourceArea.contentX
            y: sourceArea.topPadding - sourceArea.contentY
            width: Math.max(sourceArea.contentWidth, sourceArea.width)
            text: luaCodeEditor.highlightedHTML
            textFormat: Text.RichText
            font: sourceArea.font
            color: "#263238"
        }
        TextArea {
            id: sourceArea
            anchors.fill: parent
            leftPadding: 52
            rightPadding: 8
            topPadding: 8
            bottomPadding: 8
            wrapMode: TextEdit.NoWrap
            selectByMouse: true
            font.family: Qt.platform.os === "osx" ? "Menlo" : Qt.platform.os === "windows" ? "Consolas" : "DejaVu Sans Mono"
            color: "transparent"
            selectedTextColor: "transparent"
            selectionColor: "#557aa6d6"
            background: null
            cursorDelegate: Rectangle {
                width: 1
                color: "#263238"
            }
            onTextChanged: luaCodeEditor.scheduleHighlight()
        }
    }

    header: ToolBar {
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12
            Label {
                text: "GoGIS"
                font.bold: true
                font.pixelSize: 18
            }
            Label {
                text: rootWindow.tr("Desktop GIS")
                color: "#65717d"
            }
            Item {
                Layout.fillWidth: true
            }
            Button {
                objectName: "addVectorFilesButton"
                text: rootWindow.tr("Add vector files")
                onClicked: fileDialog.open()
            }
            Button {
                text: rootWindow.tr("Open workspace")
                onClicked: workspaceOpenDialog.open()
            }
            Button {
                objectName: "openAttributesButton"
                text: rootWindow.tr("Attributes")
                enabled: layerModel.count > 0
                onClicked: attributeDialog.open()
            }
            Button {
                objectName: "toggleVertexEditButton"
                text: rootWindow.vertexEditMode ? "Finish vertex edit" : "Edit vertices"
                enabled: layerModel.count > 0 && vertexHandleModel.count > 0
                onClicked: rootWindow.vertexEditMode = !rootWindow.vertexEditMode
            }
            Button {
                text: rootWindow.tr("Save GeoPackage")
                onClicked: saveDialog.open()
            }
            Button {
                text: rootWindow.tr("Save workspace")
                onClicked: workspaceDialog.open()
            }
            Button {
                objectName: "aboutButton"
                text: rootWindow.tr("About GoGIS")
                onClicked: aboutDialog.open()
            }
        }
    }

    Menu {
        id: layerContextMenu
        objectName: "layerContextMenu"
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        property string targetLayerName: ""
        MenuItem {
            objectName: "layerContextZoomToLayer"
            text: rootWindow.tr("Zoom to layer")
            onTriggered: mapViewport.zoomToLayerExtent(layerContextMenu.targetLayerName)
        }
        MenuItem {
            objectName: "layerContextGeneral"
            text: rootWindow.tr("General")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "general")
        }
        MenuItem {
            objectName: "layerContextDataSource"
            text: rootWindow.tr("Data source")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "source")
        }
        MenuItem {
            objectName: "layerContextSymbology"
            text: rootWindow.tr("Symbology")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "symbology")
        }
        MenuItem {
            objectName: "layerContextLabels"
            text: rootWindow.tr("Labels and expressions")
            onTriggered: mapViewport.openLayerPropertiesForCategory(layerContextMenu.targetLayerName, "labels")
        }
        MenuSeparator {}
        MenuItem {
            objectName: "layerContextRemove"
            text: rootWindow.tr("Remove layer…")
            onTriggered: {
                removeLayerDialog.targetLayerName = layerContextMenu.targetLayerName;
                removeLayerDialog.open();
            }
        }
    }

    Dialog {
        id: removeLayerDialog
        objectName: "removeLayerDialog"
        modal: true
        title: rootWindow.tr("Remove layer from project")
        standardButtons: Dialog.Ok | Dialog.Cancel
        width: Math.min(440, rootWindow.width - 48)
        property string targetLayerName: ""
        contentItem: Label {
            text: rootWindow.tr("Remove '%1' from this project? The source file will not be deleted.").replace("%1", removeLayerDialog.targetLayerName)
            wrapMode: Text.WordWrap
        }
        onAccepted: {
            mapCanvas.layerSettingsPayload = JSON.stringify({operation: "remove", name: targetLayerName});
            mapCanvas.layerSettingsGeneration += 1;
        }
    }

    SplitView {
        anchors.fill: parent
        anchors.margins: 12

        Frame {
            SplitView.preferredWidth: 220
            Layout.fillHeight: true
            ColumnLayout {
                anchors.fill: parent
                Label {
                    text: rootWindow.tr("Layers")
                    font.bold: true
                }
                ListView {
                    id: layerList
                    objectName: "layerList"
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: ListModel {
                        id: layerModel
                        objectName: "layerModel"
                    }
                    delegate: CheckDelegate {
                        objectName: "layerDelegate_" + name
                        width: ListView.view.width
                        text: (model.sourceError ? "⚠ " : "") + (typeof model.displayName === "undefined" || model.displayName === "" ? model.name : model.displayName)
                        checked: layerVisible
                        onToggled: {
                            if (index < 0 || index >= layerModel.count || layerModel.get(index).name !== name)
                                return;
                            layerModel.setProperty(index, "layerVisible", checked);
                            mapViewport.syncLayerVisibility();
                        }
                        onClicked: {
                            if (mapViewport.findLayerIndex(name) >= 0)
                                mapViewport.selectLayer(name);
                        }
                    }
                    MouseArea {
                        objectName: "layerContextMouseArea"
                        anchors.fill: parent
                        z: 10
                        acceptedButtons: Qt.RightButton
                        onClicked: {
                            var index = layerList.indexAt(mouse.x, mouse.y + layerList.contentY);
                            if (index >= 0)
                                mapViewport.showLayerContextMenu(layerModel.get(index).name);
                        }
                    }
                }
            }
        }

        Frame {
            SplitView.fillWidth: true
            SplitView.fillHeight: true
            background: null
            Rectangle {
                anchors.fill: parent
                z: -1
                color: "#dfe5ea"
                radius: 4
            }
            Item {
                id: mapViewport
                objectName: "mapViewport"
                anchors.fill: parent
                clip: true
                property int viewportGeneration: 0
                property real panX: 0
                property real panY: 0
                property real mapZoom: 1.0
                property string selectedLayer: ""
                property string selectedFeature: ""
                property string selectedStatus: "No feature selected"
                property string editorValue: ""
                property string attributePayloadSeen: ""
                property string layerTreePayloadSeen: "[]"
                property int mapMetadataGenerationSeen: -1
                property string activeLayer: ""
                property string pendingWorkspaceActiveLayer: ""
                property var dataBounds: [0, 0, 1, 1]
                property var fitBounds: [0, 0, 1, 1]
                property bool hasMapMetadata: false
                property bool pendingInitialLayerFit: false
                property bool hasSavedWorkspaceView: false
                property string dataCRS: ""
                property real cursorX: 0
                property real cursorY: 0
                property bool cursorValid: false
                property var attributeColumns: []
                property var attributeFieldHints: []
                property int attributePage: 0
                property int attributePageSize: 0
                property int attributeTotal: 0
                property int layerLabelGenerationSeen: -1
                property int vertexHandleGenerationSeen: -1
                property string renderStatus: "Ready"

                // mapCanvas covers the data extent while the clipped viewport
                // may show only part of it. Preserve enough zoom range for a
                // 25 m-wide view, including the canvas/viewport width ratio.
                // Projected CRSs currently follow the app's meter-unit scale
                // convention; EPSG:4326 is converted at the data extent center.
                function maxMapZoom() {
                    if (mapCanvas.width <= 0 || width <= 0 || dataBounds.length < 4)
                        return 8.0;
                    var spanX = dataBounds[2] - dataBounds[0];
                    if (!isFinite(spanX) || spanX <= 0)
                        return 8.0;
                    var targetUnits = 25.0;
                    if (dataCRS.toUpperCase() === "EPSG:4326") {
                        var latitude = (dataBounds[1] + dataBounds[3]) / 2;
                        var metersPerDegree = 111319.49 * Math.max(0.01, Math.cos(latitude * Math.PI / 180));
                        targetUnits = 25.0 / metersPerDegree;
                    }
                    var required = spanX * width / (mapCanvas.width * targetUnits);
                    if (!isFinite(required) || required <= 0)
                        return 8.0;
                    return Math.max(8.0, Math.min(1000000.0, required));
                }

                function metersPerMapUnitX(bounds, crs) {
                    if (String(crs).toUpperCase() !== "EPSG:4326")
                        return 1;
                    var latitude = (bounds[1] + bounds[3]) / 2;
                    return 111319.49 * Math.max(0.01, Math.cos(latitude * Math.PI / 180));
                }

                function mapMeterAspectRatio(bounds, crs) {
                    var spanX = bounds[2] - bounds[0];
                    var spanY = bounds[3] - bounds[1];
                    if (!isFinite(spanX) || !isFinite(spanY) || spanX <= 0 || spanY <= 0)
                        return 1;
                    return spanX * metersPerMapUnitX(bounds, crs) / (spanY * (String(crs).toUpperCase() === "EPSG:4326" ? 111319.49 : 1));
                }

                ListModel {
                    id: attributeModel
                }
                ListModel {
                    id: mapLabelModel
                    objectName: "mapLabelModel"
                }
                ListModel {
                    id: vertexHandleModel
                    objectName: "vertexHandleModel"
                }

                function anyLayerVisible() {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).layerVisible)
                            return true;
                    }
                    return false;
                }

                function selectLayer(name) {
                    if (activeLayer !== name)
                        attributeFieldHints = [];
                    activeLayer = name;
                    mapCanvas.activeLayer = name;
                    mapCanvas.activeLayerGeneration += 1;
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name) {
                            attributeLayerTabs.currentIndex = i;
                            break;
                        }
                    }
                }

                function openLayerPropertiesForCategory(name, category) {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name) {
                            selectLayer(name);
                            layerSettingsDialog.targetLayerName = name;
                            layerSettingsDialog.activeCategory = category;
                            layerSettingsDialog.open();
                            return;
                        }
                    }
                }

                function showLayerContextMenu(name) {
                    if (findLayerIndex(name) < 0)
                        return;
                    selectLayer(name);
                    layerContextMenu.targetLayerName = name;
                    layerContextMenu.popup();
                }

                function findLayerIndex(name) {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).name === name)
                            return i;
                    }
                    return -1;
                }

                function zoomToLayerExtent(name) {
                    var index = findLayerIndex(name);
                    if (index < 0 || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return false;
                    var layerBounds = [];
                    try {
                        layerBounds = JSON.parse(layerModel.get(index).boundsJson || "[]");
                    } catch (error) {
                        return false;
                    }
                    if (layerBounds.length !== 4 || dataBounds.length !== 4)
                        return false;
                    var spanX = dataBounds[2] - dataBounds[0];
                    var spanY = dataBounds[3] - dataBounds[1];
                    var layerWidth = layerBounds[2] - layerBounds[0];
                    var layerHeight = layerBounds[3] - layerBounds[1];
                    if (spanX <= 0 || spanY <= 0 || layerWidth < 0 || layerHeight < 0)
                        return false;
                    var fractionX = Math.max(layerWidth / spanX, 0.000001);
                    var fractionY = Math.max(layerHeight / spanY, 0.000001);
                    var fitZoom = Math.min(mapViewport.width / (mapCanvas.width * fractionX),
                                           mapViewport.height / (mapCanvas.height * fractionY)) * 0.9;
                    if (!isFinite(fitZoom) || fitZoom <= 0)
                        return false;
                    var centerX = (layerBounds[0] + layerBounds[2]) / 2;
                    var centerY = (layerBounds[1] + layerBounds[3]) / 2;
                    var nx = (centerX - dataBounds[0]) / spanX;
                    var ny = (centerY - dataBounds[1]) / spanY;
                    mapZoom = Math.max(0.0001, fitZoom);
                    panX = (0.5 - nx) * mapCanvas.width * mapZoom;
                    panY = (ny - 0.5) * mapCanvas.height * mapZoom;
                    pendingInitialLayerFit = false;
                    viewportGeneration += 1;
                    return true;
                }

                function zoomToFullExtent() {
                    if (mapCanvas.width <= 0 || mapCanvas.height <= 0 || width <= 0 || height <= 0)
                        return false;
                    var bounds = fitBounds.length === 4 ? fitBounds : dataBounds;
                    var spanX = dataBounds[2] - dataBounds[0];
                    var spanY = dataBounds[3] - dataBounds[1];
                    var fractionX = (bounds[2] - bounds[0]) / spanX;
                    var fractionY = (bounds[3] - bounds[1]) / spanY;
                    if (spanX <= 0 || spanY <= 0 || fractionX <= 0 || fractionY <= 0)
                        return false;
                    var fitZoom = Math.min(width / (mapCanvas.width * fractionX),
                                           height / (mapCanvas.height * fractionY)) * 0.9;
                    if (!isFinite(fitZoom) || fitZoom <= 0)
                        return false;
                    mapZoom = fitZoom;
                    var centerX = (bounds[0] + bounds[2]) / 2;
                    var centerY = (bounds[1] + bounds[3]) / 2;
                    var nx = (centerX - dataBounds[0]) / spanX;
                    var ny = (centerY - dataBounds[1]) / spanY;
                    panX = (0.5 - nx) * mapCanvas.width * mapZoom;
                    panY = (ny - 0.5) * mapCanvas.height * mapZoom;
                    pendingInitialLayerFit = false;
                    viewportGeneration += 1;
                    return true;
                }

                function fitInitialMapExtent() {
                    if (!pendingInitialLayerFit || hasSavedWorkspaceView)
                        return false;
                    if (layerModel.count === 0)
                        return false;
                    // Use the same aggregate dataBounds that defines the render
                    // canvas. Layer-specific bounds may omit a distant feature
                    // or differ from a read-only viewport's transformed extent.
                    return zoomToFullExtent();
                }

                onWidthChanged: {
                    if (pendingInitialLayerFit)
                        Qt.callLater(function() { mapViewport.fitInitialMapExtent(); });
                }
                onHeightChanged: {
                    if (pendingInitialLayerFit)
                        Qt.callLater(function() { mapViewport.fitInitialMapExtent(); });
                }

                function currentMapCoordinate() {
                    if (!cursorValid || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return "—";
                    var bounds = dataBounds;
                    var zoom = Math.max(0.0001, mapZoom);
                    var nx = 0.5 + (cursorX - mapCanvas.x - mapCanvas.width / 2) / (mapCanvas.width * zoom);
                    var ny = 0.5 - (cursorY - mapCanvas.y - mapCanvas.height / 2) / (mapCanvas.height * zoom);
                    var x = bounds[0] + nx * (bounds[2] - bounds[0]);
                    var y = bounds[1] + ny * (bounds[3] - bounds[1]);
                    var digits = dataCRS.toUpperCase() === "EPSG:4326" ? 6 : 2;
                    return "X " + Number(x).toFixed(digits) + "  Y " + Number(y).toFixed(digits);
                }

                function luaFieldAccess(name) {
                    return /^[A-Za-z_][A-Za-z0-9_]*$/.test(name) ? "feature." + name : "feature[" + JSON.stringify(name) + "]";
                }

                function luaFieldHintText() {
                    if (!attributeFieldHints.length)
                        return rootWindow.tr("Open the attribute table to inspect the layer schema.");
                    var hints = [];
                    for (var i = 0; i < attributeFieldHints.length; ++i) {
                        var field = attributeFieldHints[i];
                        hints.push(luaFieldAccess(field.name) + " : " + (field.type || "unknown"));
                    }
                    return hints.join("   ·   ");
                }

                function currentScaleText() {
                    if (mapCanvas.width <= 0 || mapZoom <= 0)
                        return "Scale —";
                    var bounds = dataBounds;
                    var xUnitsPerPixel = (bounds[2] - bounds[0]) / (mapCanvas.width * mapZoom);
                    var metersPerUnit = 1.0;
                    if (dataCRS.toUpperCase() === "EPSG:4326") {
                        var centerLatitude = (bounds[1] + bounds[3]) / 2;
                        metersPerUnit = 111319.49 * Math.max(0.01, Math.cos(centerLatitude * Math.PI / 180));
                    }
                    var denominator = xUnitsPerPixel * metersPerUnit * 96 / 0.0254;
                    if (!isFinite(denominator) || denominator <= 0)
                        return "Scale —";
                    return "Approx. 1:" + Math.max(1, Math.round(denominator)).toLocaleString();
                }

                function currentScaleDenominator() {
                    if (mapCanvas.width <= 0 || mapViewport.mapZoom <= 0)
                        return 0;
                    var bounds = mapViewport.dataBounds;
                    var unitsPerPixel = (bounds[2] - bounds[0]) / (mapCanvas.width * mapViewport.mapZoom);
                    var metersPerUnit = 1.0;
                    if (mapViewport.dataCRS.toUpperCase() === "EPSG:4326") {
                        var latitude = (bounds[1] + bounds[3]) / 2;
                        metersPerUnit = 111319.49 * Math.max(0.01, Math.cos(latitude * Math.PI / 180));
                    }
                    var denominator = unitsPerPixel * metersPerUnit * 96 / 0.0254;
                    return isFinite(denominator) && denominator > 0 ? Math.round(denominator) : 0;
                }

                function screenRotation(mapAngle) {
                    var bounds = dataBounds;
                    var spanX = bounds[2] - bounds[0];
                    var spanY = bounds[3] - bounds[1];
                    if (spanX <= 0 || spanY <= 0 || mapCanvas.width <= 0 || mapCanvas.height <= 0)
                        return -mapAngle;
                    var radians = mapAngle * Math.PI / 180;
                    var dx = Math.cos(radians) * mapCanvas.width / spanX;
                    var dy = -Math.sin(radians) * mapCanvas.height / spanY;
                    var degrees = Math.atan2(dy, dx) * 180 / Math.PI;
                    // Keep text upright regardless of source-line digitizing direction.
                    if (degrees > 90)
                        degrees -= 180;
                    if (degrees < -90)
                        degrees += 180;
                    return degrees;
                }

                function goToCoordinate() {
                    var x = Number(coordinateXInput.text);
                    var y = Number(coordinateYInput.text);
                    if (!isFinite(x) || !isFinite(y) || dataBounds.length < 4 || dataBounds[2] === dataBounds[0] || dataBounds[3] === dataBounds[1]) {
                        coordinateNavigationStatus.text = "Enter valid map coordinates";
                        return;
                    }
                    var nx = (x - dataBounds[0]) / (dataBounds[2] - dataBounds[0]);
                    var ny = (y - dataBounds[1]) / (dataBounds[3] - dataBounds[1]);
                    panX = (0.5 - nx) * mapCanvas.width * mapZoom;
                    panY = (ny - 0.5) * mapCanvas.height * mapZoom;
                    viewportGeneration += 1;
                    coordinateNavigationStatus.text = "Centered at " + x + ", " + y;
                }

                function requestAttributePage(page) {
                    var pageSize = attributePageSize > 0 ? attributePageSize : 200;
                    var pageCount = Math.max(1, Math.ceil(attributeTotal / pageSize));
                    page = Math.max(0, Math.min(pageCount - 1, page));
                    if (page === attributePage)
                        return;
                    mapCanvas.attributePage = page;
                    mapCanvas.attributePageGeneration += 1;
                }

                function syncLayerVisibility() {
                    var visibility = {};
                    for (var i = 0; i < layerModel.count; ++i) {
                        var layer = layerModel.get(i);
                        visibility[layer.name] = layer.layerVisible;
                    }
                    mapCanvas.layerVisibilityPayload = JSON.stringify(visibility);
                    mapCanvas.layerVisibilityGeneration += 1;
                }

                MapCanvas {
                    id: mapCanvas
                    objectName: "goGisMapCanvas"
                    property real mapAspectRatio: {
                        return mapViewport.mapMeterAspectRatio(mapViewport.dataBounds, mapViewport.dataCRS);
                    }
                    property real viewportPanX: mapViewport.panX
                    property real viewportPanY: mapViewport.panY
                    width: Math.min(parent.width, parent.height * mapAspectRatio)
                    height: Math.min(parent.height, parent.width / mapAspectRatio)
                    x: (parent.width - width) / 2 + mapViewport.panX
                    y: (parent.height - height) / 2 + mapViewport.panY
                    property real clickX: 0
                    property real clickY: 0
                    property int clickGeneration: 0
                    // MapCanvas geometry uses device-independent pixels.
                    property real logicalPixelsPerMm: Screen.pixelDensity > 0 && Screen.devicePixelRatio > 0 ? Screen.pixelDensity / Screen.devicePixelRatio : 96 / 25.4
                    property string selectionLayer: ""
                    property string selectionFeature: ""
                    property string selectionValue: ""
                    property string selectionStatus: "No feature selected"
                    property string layerVisibilityPayload: "{}"
                    property int layerVisibilityGeneration: 0
                    property string editAction: ""
                    property string editValue: ""
                    property int editGeneration: 0
                    property string attributePayload: "[]"
                    property int attributePage: 0
                    property int attributePageGeneration: 0
                    property string layerTreePayload: "[]"
                    property string layerSettingsPayload: ""
                    property int layerSettingsGeneration: 0
                    property string layerLabelPayload: "[]"
                    property int layerLabelGeneration: 0
                    property string vertexHandlePayload: "[]"
                    property int vertexHandleGeneration: 0
                    property string activeLayer: ""
                    property int activeLayerGeneration: 0
                    property string renderStatus: "Ready"
                    // Must be a declared QML property so updates from the
                    // native bridge notify bindings such as the Logs dialog.
                    property string diagnosticLogPayload: "[]"
                    property double processMemoryBytes: 0
                    property double goHeapBytes: 0
                    property int processMemoryKind: 0
                    property bool memoryStatusAvailable: false
                    property string mapMetadataPayload: ""
                    property int mapMetadataGeneration: 0
                    property int mapMetadataAppliedGeneration: 0
                    property int cancelGeneration: 0
                    property string loadPath: ""
                    property string loadRequestJournal: "[]"
                    property int loadGeneration: 0
                    property int loadCapturedGeneration: 0
                    onLoadCapturedGenerationChanged: {
                        var requests = JSON.parse(loadRequestJournal);
                        loadRequestJournal = JSON.stringify(requests.filter(function(request) {
                            return request.generation > loadCapturedGeneration;
                        }));
                    }
                    property string savePath: ""
                    property int saveGeneration: 0
                    scale: mapViewport.mapZoom
                    transformOrigin: Item.Center
                    visible: mapViewport.anyLayerVisible()
                }

                Repeater {
                    model: mapLabelModel
                    delegate: Text {
                        z: 2
                        x: mapCanvas.x + mapCanvas.width / 2 + (model.x - 0.5) * mapCanvas.width * mapCanvas.scale - width / 2
                        y: mapCanvas.y + mapCanvas.height / 2 - (model.y - 0.5) * mapCanvas.height * mapCanvas.scale - height / 2
                        rotation: mapViewport.screenRotation(model.rotation)
                        text: model.text
                        textFormat: Text.PlainText
                        font.pixelSize: Math.max(1, model.heightMm * mapCanvas.logicalPixelsPerMm)
                        color: "#17212b"
                        style: Text.Outline
                        styleColor: "#ffffff"
                        visible: {
                            var shown = false;
                            for (var i = 0; i < layerModel.count; ++i) {
                                var row = layerModel.get(i);
                                if (row.name === model.layer) {
                                    shown = row.layerVisible;
                                    break;
                                }
                            }
                            var denominator = mapViewport.currentScaleDenominator();
                            return shown && (model.minScale <= 0 || denominator >= model.minScale) && (model.maxScale <= 0 || denominator <= model.maxScale);
                        }
                    }
                }

                Repeater {
                    objectName: "vertexHandleRepeater"
                    z: 15
                    model: vertexHandleModel
                    delegate: Rectangle {
                        id: vertexHandle
                        property alias dragArea: vertexMouseArea
                        objectName: "vertexHandle_" + model.vertexIndex
                        z: 20
                        width: 12
                        height: 12
                        radius: 6
                        x: mapCanvas.x + mapCanvas.width / 2 + (model.x - 0.5) * mapCanvas.width * mapCanvas.scale - width / 2
                        y: mapCanvas.y + mapCanvas.height / 2 - (model.y - 0.5) * mapCanvas.height * mapCanvas.scale - height / 2
                        color: "#fff"
                        border.color: "#145da0"
                        border.width: 2
                        visible: rootWindow.vertexEditMode
                        MouseArea {
                            id: vertexMouseArea
                            objectName: "vertexDragArea_" + model.vertexIndex
                            anchors.fill: parent
                            enabled: rootWindow.vertexEditMode
                            cursorShape: Qt.SizeAllCursor
                            property real startPointerX: 0
                            property real startPointerY: 0
                            property real startHandleX: 0
                            property real startHandleY: 0
                            onPressed: function (mouse) {
                                var point = mapToItem(mapViewport, mouse.x, mouse.y);
                                startPointerX = point.x;
                                startPointerY = point.y;
                                startHandleX = vertexHandle.x;
                                startHandleY = vertexHandle.y;
                            }
                            onPositionChanged: function (mouse) {
                                if (!pressed)
                                    return;
                                var point = mapToItem(mapViewport, mouse.x, mouse.y);
                                vertexHandle.x = startHandleX + point.x - startPointerX;
                                vertexHandle.y = startHandleY + point.y - startPointerY;
                            }
                            function submitVertexEdit() {
                                var nx = 0.5 + (vertexHandle.x + vertexHandle.width / 2 - mapCanvas.x - mapCanvas.width / 2) / (mapCanvas.width * mapCanvas.scale);
                                var ny = 0.5 - (vertexHandle.y + vertexHandle.height / 2 - mapCanvas.y - mapCanvas.height / 2) / (mapCanvas.height * mapCanvas.scale);
                                var bounds = mapViewport.dataBounds;
                                mapCanvas.editAction = "moveVertex";
                                mapCanvas.editValue = JSON.stringify({vertexIndex: model.vertexIndex, x: bounds[0] + nx * (bounds[2] - bounds[0]), y: bounds[1] + ny * (bounds[3] - bounds[1])});
                                mapCanvas.editGeneration += 1;
                            }
                            onReleased: submitVertexEdit()
                        }
                    }
                }

                Component.onCompleted: mapViewport.syncLayerVisibility()

                DropArea {
                    anchors.fill: parent
                    z: 10
                    keys: ["text/uri-list"]
                    Rectangle {
                        anchors.fill: parent
                        visible: parent.containsDrag
                        color: "#337ab7"
                        opacity: 0.16
                        border.color: "#2b6cb0"
                        border.width: 2
                        radius: 4
                    }
                    Label {
                        anchors.centerIn: parent
                        visible: parent.containsDrag
                        text: rootWindow.tr("Drop SHP, GeoPackage, or GeoJSON")
                        color: "#1f4e79"
                        font.bold: true
                    }
                    onDropped: function (drop) {
                        if (drop.urls.length > 0) {
                            // Keep the QUrl intact so requestLoad can use
                            // toLocalFile() on Windows instead of passing a
                            // file:/// URI to the Go/GDAL boundary.
                            mapViewport.requestLoad(drop.urls[0]);
                            drop.acceptProposedAction();
                        }
                    }
                }

                function requestLoad(url) {
                    requestLoadFiles([url]);
                }

                function requestLoadFiles(urls) {
                    var paths = [];
                    for (var i = 0; i < urls.length; ++i) {
                        var path = localPathFromUrl(urls[i]);
                        if (path.length > 0)
                            paths.push(path);
                    }
                    var generation = mapCanvas.loadGeneration + 1;
                    var pending = JSON.parse(mapCanvas.loadRequestJournal);
                    pending.push({generation: generation, paths: paths});
                    mapCanvas.loadRequestJournal = JSON.stringify(pending);
                    mapCanvas.loadPath = JSON.stringify(paths);
                    mapCanvas.loadGeneration = generation;
                }

                function localPathFromUrl(url) {
                    var path = (url && typeof url.toLocalFile === "function") ? url.toLocalFile() : String(url);
                    if (path.indexOf("file://") === 0) {
                        path = decodeURIComponent(path.substring(7));
                        // file:///C:/... becomes /C:/... after removing the
                        // URI prefix; remove that extra slash on Windows.
                        if (path.length >= 3 && path[0] === "/" && path[2] === ":") {
                            path = path.substring(1);
                        }
                    }
                    return path;
                }

                MouseArea {
                    id: mapMouseArea
                    objectName: "mapMouseArea"
                    anchors.fill: parent
                    z: 5
                    hoverEnabled: true
                    property real lastX: 0
                    property real lastY: 0

                    onPressed: function (mouse) {
                        lastX = mouse.x;
                        lastY = mouse.y;
                    }
                    onPositionChanged: function (mouse) {
                        mapViewport.cursorX = mouse.x;
                        mapViewport.cursorY = mouse.y;
                        mapViewport.cursorValid = true;
                        if (!pressed)
                            return;
                        mapViewport.panX += mouse.x - lastX;
                        mapViewport.panY += mouse.y - lastY;
                        lastX = mouse.x;
                        lastY = mouse.y;
                        mapViewport.viewportGeneration += 1;
                    }
                    onExited: mapViewport.cursorValid = false
                    onWheel: function (wheel) {
                        // A notch advances far enough to reach parcel-level
                        // detail without dozens of wheel events and renders.
                        var factor = Math.pow(1.3, wheel.angleDelta.y / 120.0);
                        var oldZoom = mapViewport.mapZoom;
                        var newZoom = Math.max(0.25, Math.min(mapViewport.maxMapZoom(), oldZoom * factor));
                        var normalizedX = 0.5 + (wheel.x - mapCanvas.x - mapCanvas.width / 2) / (mapCanvas.width * oldZoom);
                        var normalizedY = 0.5 - (wheel.y - mapCanvas.y - mapCanvas.height / 2) / (mapCanvas.height * oldZoom);
                        var baseX = (mapViewport.width - mapCanvas.width) / 2;
                        var baseY = (mapViewport.height - mapCanvas.height) / 2;
                        mapViewport.mapZoom = newZoom;
                        mapViewport.panX = wheel.x - baseX - mapCanvas.width / 2 - (normalizedX - 0.5) * mapCanvas.width * newZoom;
                        mapViewport.panY = wheel.y - baseY - mapCanvas.height / 2 + (normalizedY - 0.5) * mapCanvas.height * newZoom;
                        mapViewport.viewportGeneration += 1;
                    }
                    onClicked: function (mouse) {
                        if (!mapViewport.mapZoom)
                            return;
                        mapCanvas.clickX = mouse.x - mapCanvas.x;
                        mapCanvas.clickY = mouse.y - mapCanvas.y;
                        mapCanvas.clickGeneration += 1;
                    }
                }

                Timer {
                    interval: 16
                    repeat: true
                    running: true
                    onTriggered: {
                        if (mapViewport.selectedFeature !== mapCanvas.selectionFeature) {
                            mapViewport.editorValue = mapCanvas.selectionValue;
                        }
                        if (mapCanvas.attributePayload !== mapViewport.attributePayloadSeen) {
                            mapViewport.attributePayloadSeen = mapCanvas.attributePayload;
                            attributeModel.clear();
                            var table = JSON.parse(mapCanvas.attributePayload);
                            mapViewport.attributeColumns = table.columns || [];
                            mapViewport.attributeFieldHints = table.fields || mapViewport.attributeColumns.map(function(name) { return {name: name, type: "unknown"}; });
                            mapViewport.attributePage = table.page || 0;
                            mapViewport.attributePageSize = table.pageSize || 0;
                            mapViewport.attributeTotal = table.total || 0;
                            var rows = table.rows || [];
                            for (var i = 0; i < rows.length; ++i) {
                                attributeModel.append(rows[i]);
                            }
                        }
                        if (mapCanvas.layerTreePayload !== mapViewport.layerTreePayloadSeen) {
                            mapViewport.layerTreePayloadSeen = mapCanvas.layerTreePayload;
                            var layers = JSON.parse(mapCanvas.layerTreePayload);
                            var previousActiveLayer = mapViewport.activeLayer;
                            layerModel.clear();
                            for (var layerIndex = 0; layerIndex < layers.length; ++layerIndex) {
                                layerModel.append({
                                    name: layers[layerIndex].name,
                                    displayName: layers[layerIndex].displayName || layers[layerIndex].name,
                                    sourcePath: layers[layerIndex].sourcePath || "",
                                    sourceLayerName: layers[layerIndex].sourceLayerName || "",
                                    sourceEncoding: layers[layerIndex].sourceEncoding || "",
                                    sourceError: layers[layerIndex].sourceError || "",
                                    crs: layers[layerIndex].crs || "",
                                    geometryType: layers[layerIndex].geometryType || "",
                                    boundsJson: JSON.stringify(layers[layerIndex].bounds || []),
                                    layerVisible: layers[layerIndex].visible !== false,
                                    style: layers[layerIndex].style || ({}),
                                    labels: layers[layerIndex].labels || ({
                                            enabled: false,
                                            expression: "",
                                            placement: "center",
                                            heightMm: 2.5
                                        })
                                });
                            }
                            if (layers.length > 0) {
                                var selectedName = layers[0].name;
                                for (var selectedIndex = 0; selectedIndex < layers.length; ++selectedIndex) {
                                    if (layers[selectedIndex].name === previousActiveLayer) {
                                        selectedName = previousActiveLayer;
                                        break;
                                    }
                                }
                                mapViewport.selectLayer(selectedName);
                            } else {
                                mapViewport.activeLayer = "";
                                mapViewport.attributeFieldHints = [];
                                mapViewport.hasMapMetadata = false;
                                layerContextMenu.close();
                            }
                            if (mapViewport.findLayerIndex(layerSettingsDialog.targetLayerName) < 0)
                                layerSettingsDialog.close();
                            if (mapViewport.findLayerIndex(layerContextMenu.targetLayerName) < 0)
                                layerContextMenu.close();
                            if (mapViewport.pendingWorkspaceActiveLayer !== "") {
                                for (var activeIndex = 0; activeIndex < layerModel.count; ++activeIndex) {
                                    if (layerModel.get(activeIndex).name === mapViewport.pendingWorkspaceActiveLayer) {
                                        mapViewport.selectLayer(mapViewport.pendingWorkspaceActiveLayer);
                                        break;
                                    }
                                }
                                mapViewport.pendingWorkspaceActiveLayer = "";
                            }
                            mapViewport.syncLayerVisibility();
                            mapViewport.fitInitialMapExtent();
                        }
                        if (mapCanvas.layerLabelGeneration !== mapViewport.layerLabelGenerationSeen) {
                            mapViewport.layerLabelGenerationSeen = mapCanvas.layerLabelGeneration;
                            mapLabelModel.clear();
                            var labels = JSON.parse(mapCanvas.layerLabelPayload || "[]");
                            if (!Array.isArray(labels))
                                labels = [];
                            for (var labelIndex = 0; labelIndex < labels.length; ++labelIndex)
                                mapLabelModel.append(labels[labelIndex]);
                        }
                        if (mapCanvas.vertexHandleGeneration !== mapViewport.vertexHandleGenerationSeen) {
                            mapViewport.vertexHandleGenerationSeen = mapCanvas.vertexHandleGeneration;
                            vertexHandleModel.clear();
                            var handles = JSON.parse(mapCanvas.vertexHandlePayload || "[]");
                            if (!Array.isArray(handles))
                                handles = [];
                            for (var handleIndex = 0; handleIndex < handles.length; ++handleIndex)
                                vertexHandleModel.append(handles[handleIndex]);
                        }
                        if (mapCanvas.mapMetadataPayload !== "" && mapCanvas.mapMetadataGeneration !== mapViewport.mapMetadataGenerationSeen) {
                            mapViewport.mapMetadataGenerationSeen = mapCanvas.mapMetadataGeneration;
                            var oldBounds = mapViewport.dataBounds;
                            var oldCRS = mapViewport.dataCRS;
                            var oldZoom = mapViewport.mapZoom;
                            var oldCanvasWidth = mapCanvas.width;
                            var oldCanvasHeight = mapCanvas.height;
                            var oldCenterX = oldBounds[0] + (0.5 - mapViewport.panX / (oldCanvasWidth * oldZoom)) * (oldBounds[2] - oldBounds[0]);
                            var oldCenterY = oldBounds[1] + (0.5 + mapViewport.panY / (oldCanvasHeight * oldZoom)) * (oldBounds[3] - oldBounds[1]);
                            var oldMetersPerPixel = (oldBounds[2] - oldBounds[0]) * mapViewport.metersPerMapUnitX(oldBounds, oldCRS) / (oldCanvasWidth * oldZoom);
                            var preserveExistingView = mapViewport.hasMapMetadata;
                            var metadata = ({});
                            try {
                                metadata = JSON.parse(mapCanvas.mapMetadataPayload);
                                if (metadata.bounds && metadata.bounds.length === 4) {
                                    mapViewport.dataBounds = metadata.bounds;
                                    mapViewport.fitBounds = metadata.fitBounds && metadata.fitBounds.length === 4 ? metadata.fitBounds : metadata.bounds;
                                    mapViewport.dataCRS = metadata.crs || "";
                                }
                            } catch (error) {
                                mapViewport.dataBounds = [0, 0, 1, 1];
                                mapViewport.fitBounds = [0, 0, 1, 1];
                                mapViewport.dataCRS = "";
                            }
                            mapViewport.hasMapMetadata = metadata.hasLayers !== false;
                            if (metadata.view) {
                                mapViewport.hasSavedWorkspaceView = true;
                                mapViewport.pendingInitialLayerFit = false;
                                var view = metadata.view;
                                var zoom = Math.max(0.0001, Number(view.zoom) || 1);
                                mapViewport.mapZoom = zoom;
                                mapViewport.panX = (0.5 - Number(view.centerX)) * mapCanvas.width * zoom;
                                mapViewport.panY = (Number(view.centerY) - 0.5) * mapCanvas.height * zoom;
                                mapViewport.pendingWorkspaceActiveLayer = view.activeLayer || "";
                                for (var viewLayerIndex = 0; viewLayerIndex < layerModel.count; ++viewLayerIndex) {
                                    if (layerModel.get(viewLayerIndex).name === mapViewport.pendingWorkspaceActiveLayer) {
                                        mapViewport.selectLayer(mapViewport.pendingWorkspaceActiveLayer);
                                        mapViewport.pendingWorkspaceActiveLayer = "";
                                        break;
                                    }
                                }
                            } else {
                                mapViewport.hasSavedWorkspaceView = false;
                                mapViewport.pendingWorkspaceActiveLayer = "";
                                var newBounds = mapViewport.dataBounds;
                                var newSpanX = newBounds[2] - newBounds[0];
                                var newSpanY = newBounds[3] - newBounds[1];
                                var newAspect = mapViewport.mapMeterAspectRatio(newBounds, mapViewport.dataCRS);
                                var newCanvasWidth = Math.min(mapViewport.width, mapViewport.height * newAspect);
                                var newCanvasHeight = Math.min(mapViewport.height, mapViewport.width / newAspect);
                                var newUnitsPerPixel = oldMetersPerPixel / mapViewport.metersPerMapUnitX(newBounds, mapViewport.dataCRS);
                                if (preserveExistingView && oldCRS === mapViewport.dataCRS && isFinite(oldCenterX) && isFinite(oldCenterY) &&
                                        isFinite(newUnitsPerPixel) && newUnitsPerPixel > 0 && newCanvasWidth > 0 && newCanvasHeight > 0 &&
                                        newSpanX > 0 && newSpanY > 0) {
                                    mapViewport.mapZoom = newSpanX / (newCanvasWidth * newUnitsPerPixel);
                                    mapViewport.panX = (0.5 - (oldCenterX - newBounds[0]) / newSpanX) * newCanvasWidth * mapViewport.mapZoom;
                                    mapViewport.panY = ((oldCenterY - newBounds[1]) / newSpanY - 0.5) * newCanvasHeight * mapViewport.mapZoom;
                                    mapViewport.pendingInitialLayerFit = false;
                                } else {
                                    mapViewport.panX = 0;
                                    mapViewport.panY = 0;
                                    mapViewport.mapZoom = 1;
                                    mapViewport.pendingInitialLayerFit = true;
                                    mapViewport.fitInitialMapExtent();
                                }
                            }
                            mapViewport.viewportGeneration += 1;
                            mapCanvas.mapMetadataAppliedGeneration = mapCanvas.mapMetadataGeneration;
                        }
                        mapViewport.renderStatus = mapCanvas.renderStatus;
                        mapViewport.selectedLayer = mapCanvas.selectionLayer;
                        mapViewport.selectedFeature = mapCanvas.selectionFeature;
                        mapViewport.selectedStatus = mapCanvas.selectionStatus;
                    }
                }

                Label {
                    anchors.left: parent.left
                    anchors.bottom: parent.bottom
                    anchors.margins: 10
                    text: rootWindow.tr("Drag to pan · Scroll to zoom · Click a feature to inspect")
                    color: "#65717d"
                }
                ColumnLayout {
                    anchors.centerIn: parent
                    // Keep the empty-state action above the full-map pan/click area.
                    z: 6
                    visible: layerModel.count === 0
                    spacing: 8
                    Label {
                        Layout.alignment: Qt.AlignHCenter
                        text: rootWindow.tr("No layers yet")
                        font.pixelSize: 22
                        font.bold: true
                        color: "#45515c"
                    }
                    Label {
                        Layout.alignment: Qt.AlignHCenter
                        text: rootWindow.tr("Add vector files or open a workspace to begin.")
                        color: "#65717d"
                    }
                    Button {
                        objectName: "emptyStateAddVectorFilesButton"
                        Layout.alignment: Qt.AlignHCenter
                        text: rootWindow.tr("Add vector files")
                        onClicked: fileDialog.open()
                    }
                }

                Button {
                    objectName: "zoomToFullExtentButton"
                    anchors.top: parent.top
                    anchors.right: parent.right
                    anchors.margins: 10
                    z: 6
                    visible: layerModel.count > 0
                    text: rootWindow.tr("Zoom to full extent")
                    onClicked: mapViewport.zoomToFullExtent()
                }
            }
        }
    }

    Dialog {
        id: aboutDialog
        objectName: "aboutDialog"
        modal: true
        title: rootWindow.tr("About GoGIS")
        width: Math.min(460, rootWindow.width - 48)
        standardButtons: Dialog.Close
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                text: "GoGIS"
                font.pixelSize: 22
                font.bold: true
            }
            GridLayout {
                columns: 2
                Label { text: rootWindow.tr("Version") }
                Label { objectName: "aboutVersionValue"; text: rootWindow.versionText }
                Label { text: rootWindow.tr("Build") }
                Label { objectName: "aboutBuildValue"; text: rootWindow.buildTargetText }
                Label { text: rootWindow.tr("Runtime") }
                Label { objectName: "aboutRuntimeValue"; text: rootWindow.runtimeText }
                Label { text: rootWindow.tr("License") }
                Label { text: "MIT" }
            }
        }
    }

    Dialog {
        id: attributeDialog
        objectName: "attributeDialog"
        modal: true
        title: rootWindow.tr("Attributes") + " — " + mapViewport.activeLayer
        width: Math.min(780, rootWindow.width - 48)
        height: Math.min(680, rootWindow.height - 80)
        standardButtons: Dialog.Close
        contentItem: ColumnLayout {
            spacing: 8
                Flickable {
                    Layout.fillWidth: true
                    Layout.preferredHeight: attributeLayerTabs.implicitHeight
                    contentWidth: attributeLayerTabs.implicitWidth
                    clip: true
                    boundsBehavior: Flickable.StopAtBounds
                    TabBar {
                        id: attributeLayerTabs
                        width: implicitWidth
                        Repeater {
                            model: layerModel
                            TabButton {
                                text: typeof model.displayName === "undefined" || model.displayName === "" ? model.name : model.displayName
                                onClicked: mapViewport.selectLayer(model.name)
                            }
                        }
                    }
                }
                Label {
                    text: rootWindow.tr("Selected feature")
                    font.bold: true
                    visible: mapViewport.selectedFeature !== ""
                }
                Label {
                    text: mapViewport.selectedStatus
                    wrapMode: Text.WordWrap
                    visible: mapViewport.selectedFeature !== ""
                    color: mapViewport.selectedFeature ? "#2e7d32" : "#65717d"
                }
                TextField {
                    id: propertyEditor
                    Layout.fillWidth: true
                    visible: mapViewport.selectedFeature !== ""
                    placeholderText: rootWindow.tr("Feature name")
                    text: mapViewport.editorValue
                    onTextChanged: if (activeFocus)
                        mapViewport.editorValue = text
                }
                RowLayout {
                    visible: mapViewport.selectedFeature !== ""
                    Button {
                        text: rootWindow.tr("Save")
                        onClicked: {
                            mapCanvas.editAction = "commit";
                            mapCanvas.editValue = mapViewport.editorValue;
                            mapCanvas.editGeneration += 1;
                        }
                    }
                    Button {
                        text: rootWindow.tr("Cancel")
                        onClicked: {
                            mapCanvas.editAction = "rollback";
                            mapCanvas.editValue = "";
                            mapCanvas.editGeneration += 1;
                            mapViewport.editorValue = mapCanvas.selectionValue;
                        }
                    }
                }
                GridLayout {
                    columns: 2
                    visible: mapViewport.selectedFeature !== ""
                    Label {
                        text: "Layer"
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.selectedLayer
                    }
                    Label {
                        text: "Feature"
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.selectedFeature
                    }
                    Label {
                        text: "Editable"
                        font.bold: true
                    }
                    Label {
                        text: "Yes"
                        color: "#2e7d32"
                    }
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: mapViewport.attributeTotal > mapViewport.attributePageSize && mapViewport.attributePageSize > 0
                    Button {
                        text: rootWindow.tr("Previous")
                        enabled: mapViewport.attributePage > 0
                        onClicked: mapViewport.requestAttributePage(mapViewport.attributePage - 1)
                    }
                    Label {
                        text: "Page " + (mapViewport.attributePage + 1) + " / " + Math.max(1, Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize))
                        Layout.fillWidth: true
                        horizontalAlignment: Text.AlignHCenter
                    }
                    Button {
                        text: rootWindow.tr("Next")
                        enabled: mapViewport.attributePage + 1 < Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize)
                        onClicked: mapViewport.requestAttributePage(mapViewport.attributePage + 1)
                    }
                }
                ListView {
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    visible: layerModel.count > 0 && mapViewport.attributeTotal > 0
                    model: attributeModel
                    clip: true
                    spacing: 6
                    delegate: Rectangle {
                        id: featureCard
                        width: ListView.view.width
                        property var rowValues: model.values || ({})
                        property var rowFeatureID: model.featureId
                        implicitHeight: cardLayout.implicitHeight + 16
                        color: "#ffffff"
                        border.color: "#d9e0e6"
                        radius: 4
                        ColumnLayout {
                            id: cardLayout
                            anchors.fill: parent
                            anchors.margins: 8
                            spacing: 4
                            Label {
                                text: "Feature " + String(featureCard.rowFeatureID)
                                font.bold: true
                                color: "#45515c"
                            }
                            Repeater {
                                model: mapViewport.attributeColumns
                                delegate: RowLayout {
                                    required property string modelData
                                    width: parent.width
                                    spacing: 8
                                    Label {
                                        text: modelData
                                        Layout.preferredWidth: 82
                                        Layout.minimumWidth: 82
                                        color: "#65717d"
                                        elide: Text.ElideRight
                                    }
                                    Label {
                                        Layout.fillWidth: true
                                        text: {
                                            var value = featureCard.rowValues[modelData];
                                            return value === undefined || value === null ? "" : String(value);
                                        }
                                        wrapMode: Text.Wrap
                                        textFormat: Text.PlainText
                                    }
                                }
                            }
                        }
                    }
                }
                Label {
                    Layout.fillWidth: true
                    visible: layerModel.count > 0 && mapViewport.attributeTotal === 0
                    text: rootWindow.tr("No attribute records in this layer")
                    color: "#65717d"
                    horizontalAlignment: Text.AlignHCenter
                    padding: 16
                }
                Item { Layout.fillHeight: true }
        }
    }

    footer: ToolBar {
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12
            Button {
                objectName: "openDiagnosticLogsButton"
                text: rootWindow.tr("Logs")
                onClicked: diagnosticLogDialog.open()
            }
            BusyIndicator {
                objectName: "loadBusyIndicator"
                running: rootWindow.statusIsBusy(mapViewport.renderStatus)
                visible: running
                implicitWidth: 22
                implicitHeight: 22
            }
            Label {
                objectName: "renderStatusLabel"
                text: rootWindow.localizedStatus(mapViewport.renderStatus)
                color: rootWindow.statusColor(mapViewport.renderStatus)
                Layout.maximumWidth: 390
                elide: Text.ElideRight
            }
            TextField {
                id: coordinateXInput
                Layout.preferredWidth: 115
                placeholderText: rootWindow.tr("X coordinate")
            }
            TextField {
                id: coordinateYInput
                Layout.preferredWidth: 115
                placeholderText: rootWindow.tr("Y coordinate")
                onAccepted: mapViewport.goToCoordinate()
            }
            Button {
                text: rootWindow.tr("Go")
                onClicked: mapViewport.goToCoordinate()
            }
            Label {
                id: coordinateNavigationStatus
                color: "#65717d"
            }
            Button {
                text: rootWindow.tr("Cancel loading/render")
                enabled: mapViewport.renderStatus.toLowerCase().indexOf("loading") >= 0
                onClicked: mapCanvas.cancelGeneration += 1
            }
            Item {
                Layout.fillWidth: true
            }
            Label {
                objectName: "memoryStatusLabel"
                visible: rootWindow.width >= 1200
                text: rootWindow.memorySummary(mapCanvas.processMemoryBytes, mapCanvas.goHeapBytes, mapCanvas.memoryStatusAvailable, mapCanvas.processMemoryKind)
                color: "#65717d"
                font.pixelSize: 11
                Layout.maximumWidth: 240
                elide: Text.ElideRight
                ToolTip.visible: memoryMouse.containsMouse
                ToolTip.text: language === "ko" ? "프로세스 메모리에는 Go/GDAL/Qt가 포함됩니다. GPU 메모리는 포함되지 않습니다. macOS CGO 데스크톱은 현재 RSS를 표시합니다." : language === "jp" ? "プロセスメモリにはGo/GDAL/Qtを含みます。GPUメモリは含みません。macOSのCGOデスクトップは現在のRSSを表示します。" : "Process memory includes Go/GDAL/Qt, but excludes GPU memory. The macOS CGO desktop shows current RSS."
                MouseArea { id: memoryMouse; anchors.fill: parent; hoverEnabled: true }
            }
            Label {
                text: "Layers: " + layerModel.count + "  ·  " + (mapViewport.dataCRS || "CRS unknown") + "  ·  " + mapViewport.currentMapCoordinate()
                color: "#65717d"
            }
            Label {
                text: mapViewport.currentScaleText()
                color: "#65717d"
            }
        }
    }

    Dialog {
        id: layerSettingsDialog
        objectName: "layerSettingsDialog"
        modal: true
        title: rootWindow.tr("Layer properties") + " — " + targetLayerName
        width: Math.min(720, rootWindow.width - 48)
        height: Math.max(320, Math.min(760, rootWindow.height - 80))
        standardButtons: Dialog.Apply | Dialog.Cancel
        property string targetLayerName: ""
        property string originalSourcePath: ""
        property string originalSourceLayerName: ""
        property string originalSourceEncoding: ""
        property string labelLuaSource: ""
        property string activeCategory: "general"
        property string targetGeometryType: ""
        property real previousFillOpacity: 0.35
        property real labelCaptionWidth: Math.min(180, Math.max(120, layerSettingsScroll.availableWidth * 0.25))
        onActiveCategoryChanged: {
            if (layerSettingsScroll.contentItem)
                layerSettingsScroll.contentItem.contentY = 0;
        }

        function insertLabelField(fieldName) {
            var reference = "${" + fieldName + "}";
            var position = labelExpressionField.cursorPosition;
            labelExpressionField.insert(position, reference);
            labelExpressionField.cursorPosition = position + reference.length;
            labelExpressionField.forceActiveFocus();
        }

        function styleAppliesTo(kind) {
            var type = targetGeometryType.toUpperCase();
            if (type === "" || type.indexOf("COLLECTION") >= 0)
                return true;
            if (kind === "point")
                return type.indexOf("POINT") >= 0;
            if (kind === "line")
                return type.indexOf("LINE") >= 0 || type.indexOf("POLYGON") >= 0;
            return type.indexOf("POLYGON") >= 0;
        }

        function loadLayer() {
            if (layerModel.count === 0)
                return;
            var rowIndex = Math.max(0, Math.min(attributeLayerTabs.currentIndex, layerModel.count - 1));
            var layer = layerModel.get(rowIndex);
            targetLayerName = layer.name;
            targetGeometryType = layer.geometryType || "";
            displayNameField.text = layer.displayName || layer.name;
            sourcePathField.text = layer.sourcePath || "";
            originalSourcePath = sourcePathField.text.trim();
            sourceStatusLabel.text = layer.sourceError || "";
            sourceLayerField.text = layer.sourceLayerName || layer.name;
            originalSourceLayerName = sourceLayerField.text.trim();
            sourceEncodingField.currentIndex = -1;
            sourceEncodingField.editText = layer.sourceEncoding || "";
            originalSourceEncoding = sourceEncodingField.editText.trim();
            visibleField.checked = layer.layerVisible;
            var style = layer.style || ({});
            pointColorField.text = style.pointColor || "#d1495b";
            lineColorField.text = style.lineColor || "#2b6cb0";
            polygonColorField.text = style.polygonColor || "#356b53";
            pointSizeField.text = String(style.pointSizeMm || 2.2);
            lineWidthField.text = String(style.lineWidthMm || 0.45);
            fillOpacityField.text = String(style.fillOpacity === undefined ? 0.35 : style.fillOpacity);
            previousFillOpacity = Number(fillOpacityField.text) > 0 ? Number(fillOpacityField.text) : 0.35;
            var labels = layer.labels || ({});
            labelsEnabledField.checked = labels.enabled === true;
            labelExpressionField.text = labels.expression || "";
            labelPlacementField.currentIndex = Math.max(0, ["center", "center-rotated", "free-angle"].indexOf(labels.placement || "center"));
            labelRotationField.text = labels.rotationField || "";
            labelHeightField.text = String(labels.heightMm || 2.5);
            labelMinScaleField.text = String(labels.minScale || "");
            labelMaxScaleField.text = String(labels.maxScale || "");
            labelRuleField.text = labels.rule || "";
            labelLuaSource = labels.luaScript || "";
        }

        function submitLayer() {
            var index = -1;
            for (var i = 0; i < layerModel.count; ++i) {
                if (layerModel.get(i).name === targetLayerName) {
                    index = i;
                    break;
                }
            }
            if (index < 0)
                return;
            var settings = {
                name: targetLayerName,
                displayName: displayNameField.text.trim(),
                sourcePath: sourcePathField.text,
                sourceLayerName: sourceLayerField.text,
                sourceEncoding: sourceEncodingField.editText.trim(),
                visible: visibleField.checked,
                style: {
                    pointColor: pointColorField.text,
                    lineColor: lineColorField.text,
                    polygonColor: polygonColorField.text,
                    pointSizeMm: Number(pointSizeField.text),
                    lineWidthMm: Number(lineWidthField.text),
                    fillOpacity: Number(fillOpacityField.text)
                },
                labels: {
                    enabled: labelsEnabledField.checked,
                    expression: labelExpressionField.text,
                    luaScript: labelLuaSource,
                    placement: ["center", "center-rotated", "free-angle"][labelPlacementField.currentIndex],
                    rotationField: labelRotationField.text,
                    heightMm: Number(labelHeightField.text),
                    minScale: Number(labelMinScaleField.text || 0),
                    maxScale: Number(labelMaxScaleField.text || 0),
                    rule: labelRuleField.text
                }
            };
            // Keep the visible layer tree authoritative. The runtime validates
            // and applies the request, then publishes a refreshed tree; an
            // optimistic edit here would make rejected settings look saved.
            mapCanvas.layerSettingsPayload = JSON.stringify(settings);
            mapCanvas.layerSettingsGeneration += 1;
        }

        onOpened: loadLayer()
        onApplied: submitLayer()
        onAccepted: submitLayer()

        contentItem: ScrollView {
            id: layerSettingsScroll
            objectName: "layerSettingsScroll"
            clip: true
            contentWidth: availableWidth
            contentHeight: layerSettingsColumn.implicitHeight
            ColumnLayout {
                id: layerSettingsColumn
                width: layerSettingsScroll.availableWidth
                height: implicitHeight
                spacing: 8
                TabBar {
                    id: layerCategoryTabs
                    objectName: "layerCategoryTabs"
                    Layout.fillWidth: true
                    currentIndex: Math.max(0, ["general", "source", "symbology", "labels"].indexOf(layerSettingsDialog.activeCategory))
                    onCurrentIndexChanged: {
                        if (currentIndex >= 0)
                            layerSettingsDialog.activeCategory = ["general", "source", "symbology", "labels"][currentIndex];
                    }
                    TabButton { width: layerSettingsScroll.availableWidth / 4; text: rootWindow.tr("General") }
                    TabButton { width: layerSettingsScroll.availableWidth / 4; text: rootWindow.tr("Data source") }
                    TabButton { width: layerSettingsScroll.availableWidth / 4; text: rootWindow.tr("Symbology") }
                    TabButton { width: layerSettingsScroll.availableWidth / 4; text: rootWindow.tr("Labels and expressions") }
                }
                Label {
                    text: rootWindow.tr("General")
                    font.bold: true
                    visible: layerSettingsDialog.activeCategory === "general"
                }
                TextField {
                    id: displayNameField
                    objectName: "displayNameField"
                    Layout.fillWidth: true
                    placeholderText: rootWindow.tr("Display name")
                    visible: layerSettingsDialog.activeCategory === "general"
                }
                CheckBox {
                    id: visibleField
                    objectName: "visibleField"
                    text: rootWindow.tr("Layer visible")
                    visible: layerSettingsDialog.activeCategory === "general"
                }
                Label {
                    text: rootWindow.tr("Data source")
                    font.bold: true
                    visible: layerSettingsDialog.activeCategory === "source"
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source"
                    TextField {
                        id: sourcePathField
                        objectName: "sourcePathField"
                        Layout.fillWidth: true
                        placeholderText: rootWindow.tr("Original source path")
                    }
                    Button {
                        text: rootWindow.tr("Browse…")
                        onClicked: relinkFileDialog.open()
                    }
                }
                Label {
                    id: sourceStatusLabel
                    Layout.fillWidth: true
                    color: "#b45309"
                    wrapMode: Text.Wrap
                    textFormat: Text.PlainText
                    visible: layerSettingsDialog.activeCategory === "source" && text !== ""
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source"
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label {
                            text: rootWindow.tr("Layer in source")
                            color: "#65717d"
                        }
                        TextField {
                            id: sourceLayerField
                            objectName: "sourceLayerField"
                            Layout.fillWidth: true
                            placeholderText: rootWindow.tr("Internal layer name")
                        }
                    }
                    ColumnLayout {
                        Layout.preferredWidth: 180
                        Label {
                            text: rootWindow.tr("Shapefile encoding")
                            color: "#65717d"
                        }
                        ComboBox {
                            id: sourceEncodingField
                            objectName: "sourceEncodingField"
                            Layout.fillWidth: true
                            editable: true
                            model: ["", "UTF-8", "CP949", "EUC-KR", "ISO-8859-1"]
                    displayText: currentText === "" ? rootWindow.tr("Auto encoding") : currentText
                        }
                    }
                }
                Label {
                    objectName: "sourceChangeWarning"
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source" && (sourcePathField.text.trim() !== layerSettingsDialog.originalSourcePath || sourceLayerField.text.trim() !== layerSettingsDialog.originalSourceLayerName || sourceEncodingField.editText.trim() !== layerSettingsDialog.originalSourceEncoding)
                    text: "Changing the source path, internal layer, or encoding reloads this layer. Save unsaved feature edits in it first."
                    color: "#9a6700"
                    wrapMode: Text.Wrap
                    textFormat: Text.PlainText
                }
                Label {
                    text: rootWindow.tr("Symbology")
                    font.bold: true
                    topPadding: 8
                    visible: layerSettingsDialog.activeCategory === "symbology"
                }
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.tr("Set point symbols, boundary lines, and polygon fill. Colors use #RRGGBB; sizes are in millimeters on screen.")
                    wrapMode: Text.WordWrap
                    color: "#65717d"
                    visible: layerSettingsDialog.activeCategory === "symbology"
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2
                    visible: layerSettingsDialog.activeCategory === "symbology"
                    Label {
                        text: rootWindow.tr("Point color")
                        visible: layerSettingsDialog.styleAppliesTo("point")
                    }
                    TextField {
                        id: pointColorField
                        objectName: "pointColorField"
                        Layout.fillWidth: true
                        visible: layerSettingsDialog.styleAppliesTo("point")
                    }
                    Label {
                        text: rootWindow.tr("Point size (mm)")
                        visible: layerSettingsDialog.styleAppliesTo("point")
                    }
                    TextField {
                        id: pointSizeField
                        objectName: "pointSizeField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                        visible: layerSettingsDialog.styleAppliesTo("point")
                    }
                    Label {
                        text: rootWindow.tr("Line color")
                        visible: layerSettingsDialog.styleAppliesTo("line")
                    }
                    TextField {
                        id: lineColorField
                        objectName: "lineColorField"
                        Layout.fillWidth: true
                        visible: layerSettingsDialog.styleAppliesTo("line")
                    }
                    Label {
                        text: rootWindow.tr("Line width (mm)")
                        visible: layerSettingsDialog.styleAppliesTo("line")
                    }
                    TextField {
                        id: lineWidthField
                        objectName: "lineWidthField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                        visible: layerSettingsDialog.styleAppliesTo("line")
                    }
                    Label {
                        text: rootWindow.tr("Polygon color")
                        visible: layerSettingsDialog.styleAppliesTo("polygon")
                    }
                    TextField {
                        id: polygonColorField
                        objectName: "polygonColorField"
                        Layout.fillWidth: true
                        visible: layerSettingsDialog.styleAppliesTo("polygon")
                    }
                    Label {
                        text: rootWindow.tr("Fill opacity (0–1)")
                        visible: layerSettingsDialog.styleAppliesTo("polygon")
                    }
                    TextField {
                        id: fillOpacityField
                        objectName: "fillOpacityField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                        visible: layerSettingsDialog.styleAppliesTo("polygon")
                    }
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "symbology" && layerSettingsDialog.styleAppliesTo("polygon")
                    Label { text: rootWindow.tr("Transparent") }
                    Slider {
                        objectName: "fillOpacitySlider"
                        Layout.fillWidth: true
                        from: 0
                        to: 100
                        value: Math.max(0, Math.min(100, Number(fillOpacityField.text) * 100))
                        onMoved: fillOpacityField.text = String(Math.round(value) / 100)
                    }
                    Label { text: rootWindow.tr("Opaque") }
                }
                CheckBox {
                    id: outlineOnlyField
                    objectName: "outlineOnlyField"
                    text: rootWindow.tr("Outline only (no polygon fill)")
                    checked: Number(fillOpacityField.text) === 0
                    visible: layerSettingsDialog.activeCategory === "symbology" && layerSettingsDialog.styleAppliesTo("polygon")
                    onClicked: {
                        if (checked) {
                            if (Number(fillOpacityField.text) > 0)
                                layerSettingsDialog.previousFillOpacity = Number(fillOpacityField.text);
                            fillOpacityField.text = "0";
                        } else {
                            fillOpacityField.text = String(layerSettingsDialog.previousFillOpacity);
                        }
                    }
                }
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.tr("Fill opacity: 0 = transparent, 1 = opaque. Boundary lines remain visible. This setting affects polygons only.")
                    wrapMode: Text.WordWrap
                    color: "#65717d"
                    visible: layerSettingsDialog.activeCategory === "symbology" && layerSettingsDialog.styleAppliesTo("polygon")
                }
                Label {
                    text: rootWindow.tr("Labels and expressions")
                    font.bold: true
                    topPadding: 8
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                CheckBox {
                    id: labelsEnabledField
                    objectName: "labelsEnabledField"
                    text: rootWindow.tr("Show labels")
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                RowLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "labels"
                    Label {
                        Layout.fillWidth: true
                        text: rootWindow.tr("Label field / template")
                    }
                    Button {
                        id: labelFieldHintButton
                        objectName: "labelFieldHintButton"
                        text: rootWindow.tr("Available fields…")
                        onClicked: labelFieldHintPopup.open()
                    }
                }
                TextField {
                    id: labelExpressionField
                    objectName: "labelExpressionField"
                    Layout.fillWidth: true
                    placeholderText: "e.g. ${name}"
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.tr("Choose a field or type a template, for example ${NAME} (${CODE}).")
                    wrapMode: Text.WordWrap
                    color: "#65717d"
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    visible: layerSettingsDialog.activeCategory === "labels"
                    Label {
                        text: rootWindow.tr("Placement")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    ComboBox {
                        id: labelPlacementField
                        objectName: "labelPlacementField"
                        Layout.fillWidth: true
                        model: [rootWindow.tr("Center"), rootWindow.tr("Center + rotation"), rootWindow.tr("Free angle")]
                    }
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    visible: layerSettingsDialog.activeCategory === "labels"
                    Label {
                        text: rootWindow.tr("Rotation field (optional)")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    TextField {
                        id: labelRotationField
                        objectName: "labelRotationField"
                        Layout.fillWidth: true
                        placeholderText: rootWindow.tr("Rotation field (optional)")
                    }
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2
                    columnSpacing: 8
                    visible: layerSettingsDialog.activeCategory === "labels"
                    Label {
                        text: rootWindow.tr("Text height (mm)")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    TextField {
                        id: labelHeightField
                        objectName: "labelHeightField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        id: labelMinScaleCaption
                        text: rootWindow.tr("Minimum scale denominator")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    TextField {
                        id: labelMinScaleField
                        objectName: "labelMinScaleField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                    Label {
                        text: rootWindow.tr("Maximum scale denominator")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    TextField {
                        id: labelMaxScaleField
                        objectName: "labelMaxScaleField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
                    }
                }
                Label {
                    text: rootWindow.tr("Display rule — return true to show this feature's label") + " (Lua)"
                    wrapMode: Text.WordWrap
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                TextField {
                    id: labelRuleField
                    objectName: "labelRuleField"
                    Layout.fillWidth: true
                    placeholderText: "e.g. return feature.CLASS == \"primary\""
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Label {
                    text: rootWindow.tr("Label text — return string, number, or nil") + " (Lua)"
                    wrapMode: Text.WordWrap
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.tr("Lua field types") + ": " + mapViewport.luaFieldHintText()
                    color: "#65717d"
                    wrapMode: Text.WordWrap
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
                Button {
                    objectName: "openLuaEditorButton"
                    text: rootWindow.tr("Open Lua editor and examples…")
                    Layout.fillWidth: true
                    onClicked: luaEditorDialog.open()
                    visible: layerSettingsDialog.activeCategory === "labels"
                }
            }
        }
    }

    Popup {
        id: labelFieldHintPopup
        objectName: "labelFieldHintPopup"
        parent: Overlay.overlay
        anchors.centerIn: Overlay.overlay
        modal: true
        width: Math.min(460, rootWindow.width - 64)
        height: Math.min(450, rootWindow.height - 96)
        padding: 16
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Fields for this layer")
                font.bold: true
            }
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Click a field to insert ${FIELD} at the cursor. A single field name also works.")
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            Label {
                Layout.fillWidth: true
                visible: mapViewport.attributeFieldHints.length === 0
                text: rootWindow.tr("Field list is loading or unavailable for this layer.")
                wrapMode: Text.WordWrap
            }
            ListView {
                id: labelFieldList
                objectName: "labelFieldList"
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                model: mapViewport.attributeFieldHints
                spacing: 4
                delegate: Button {
                    objectName: "labelFieldHintRow"
                    width: labelFieldList.width
                    text: modelData.name + "  ·  " + (modelData.type || "unknown")
                    onClicked: {
                        layerSettingsDialog.insertLabelField(modelData.name);
                        labelFieldHintPopup.close();
                    }
                }
            }
            Button {
                Layout.alignment: Qt.AlignRight
                text: rootWindow.tr("Close")
                onClicked: labelFieldHintPopup.close()
            }
        }
    }

    Dialog {
        id: luaEditorDialog
        objectName: "luaEditorDialog"
        modal: true
        title: rootWindow.tr("Lua label editor") + " — " + layerSettingsDialog.targetLayerName
        width: Math.min(760, rootWindow.width - 64)
        height: Math.min(680, rootWindow.height - 80)
        standardButtons: Dialog.Ok | Dialog.Cancel
        onOpened: {
            labelRuleEditor.text = labelRuleField.text;
            labelLuaField.text = layerSettingsDialog.labelLuaSource;
        }
        onAccepted: {
            labelRuleField.text = labelRuleEditor.text;
            layerSettingsDialog.labelLuaSource = labelLuaField.text;
        }
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.")
                wrapMode: Text.WordWrap
                color: "#45515c"
            }
            Label {
                text: rootWindow.tr("Lua field access hint").replace("%1", mapViewport.luaFieldHintText())
                Layout.fillWidth: true
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Lua API help")
                wrapMode: Text.WordWrap
                color: "#45515c"
            }
            Label {
                text: rootWindow.tr("Display rule — return true to show this feature's label")
                font.bold: true
            }
            LuaCodeEditor {
                id: labelRuleEditor
                objectName: "labelRuleEditor"
                Layout.fillWidth: true
                Layout.preferredHeight: 100
                placeholderText: "return feature.CLASS == \"primary\""
            }
            ComboBox {
                objectName: "insertLuaRuleFieldCombo"
                Layout.fillWidth: true
                model: mapViewport.attributeFieldHints
                textRole: "name"
                enabled: mapViewport.attributeFieldHints.length > 0
                displayText: rootWindow.tr("Insert field into rule…")
                onActivated: function(index) {
                    var fieldName = mapViewport.attributeFieldHints[index].name;
                    labelRuleEditor.insert(labelRuleEditor.cursorPosition, mapViewport.luaFieldAccess(fieldName));
                    currentIndex = -1;
                }
            }
            Label {
                text: rootWindow.tr("Label text — return string, number, or nil")
                font.bold: true
            }
            LuaCodeEditor {
                id: labelLuaField
                objectName: "labelLuaField"
                Layout.fillWidth: true
                Layout.fillHeight: true
                placeholderText: "return string.format(\"%s (%s)\", feature.NAME, feature.CLASS)"
            }
            ComboBox {
                objectName: "insertLuaLabelFieldCombo"
                Layout.fillWidth: true
                model: mapViewport.attributeFieldHints
                textRole: "name"
                enabled: mapViewport.attributeFieldHints.length > 0
                displayText: rootWindow.tr("Insert field into label…")
                onActivated: function(index) {
                    var fieldName = mapViewport.attributeFieldHints[index].name;
                    labelLuaField.insert(labelLuaField.cursorPosition, mapViewport.luaFieldAccess(fieldName));
                    currentIndex = -1;
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Label { Layout.fillWidth: true }
                Button {
                    text: rootWindow.tr("Insert label example")
                    onClicked: labelLuaField.text = "return string.format(\"%s (%s)\", feature.NAME, feature.CLASS)"
                }
                Button {
                    text: rootWindow.tr("Insert rule example")
                    onClicked: labelRuleEditor.text = "return feature.CLASS == \"primary\""
                }
            }
        }
    }

    Dialog {
        id: diagnosticLogDialog
        objectName: "diagnosticLogDialog"
        title: rootWindow.tr("Application log")
        modal: true
        width: Math.min(980, rootWindow.width - 48)
        height: Math.min(680, rootWindow.height - 48)
        anchors.centerIn: Overlay.overlay
        standardButtons: Dialog.Close
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Showing the most recent process output and application errors. Older entries are discarded.")
                color: "#65717d"
                wrapMode: Text.Wrap
            }
            ScrollView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                TextArea {
                    objectName: "diagnosticLogTextArea"
                    readOnly: true
                    selectByMouse: true
                    wrapMode: TextEdit.NoWrap
                    textFormat: TextEdit.PlainText
                    text: {
                        try {
                            var entries = JSON.parse(mapCanvas.diagnosticLogPayload || "[]");
                            return entries.map(function(entry) {
                                return "[" + entry.time + "] " + entry.stream + ": " + entry.message;
                            }).join("\n");
                        } catch (error) {
                            return String(mapCanvas.diagnosticLogPayload || "");
                        }
                    }
                    onTextChanged: cursorPosition = length
                }
            }
        }
    }

    QuickDialogs.FileDialog {
        id: fileDialog
        objectName: "addVectorFilesDialog"
        title: rootWindow.tr("Add vector files as layers")
        fileMode: QuickDialogs.FileDialog.OpenFiles
        nameFilters: ["Vector files (*.shp *.gpkg *.geojson *.json *.dxf)", "All files (*)"]
        onAccepted: mapViewport.requestLoadFiles(selectedFiles)
    }

    Platform.FileDialog {
        id: saveDialog
        title: "Save all layers as GeoPackage"
        fileMode: Platform.FileDialog.SaveFile
        nameFilters: ["GeoPackage (*.gpkg)"]
        onAccepted: {
            var path = mapViewport.localPathFromUrl(file);
            if (!path.toLowerCase().endsWith(".gpkg"))
                path += ".gpkg";
            mapCanvas.savePath = path;
            mapCanvas.saveGeneration += 1;
        }
    }

    Platform.FileDialog {
        id: workspaceDialog
        title: rootWindow.tr("Save GoGIS workspace")
        fileMode: Platform.FileDialog.SaveFile
        nameFilters: ["GoGIS workspace (*.gogis)"]
        onAccepted: {
            var path = mapViewport.localPathFromUrl(file);
            if (!path.toLowerCase().endsWith(".gogis"))
                path += ".gogis";
            mapCanvas.savePath = path;
            mapCanvas.saveGeneration += 1;
        }
    }

    Platform.FileDialog {
        id: workspaceOpenDialog
        title: rootWindow.tr("Open GoGIS workspace")
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["GoGIS workspace (*.gogis)"]
        onAccepted: mapViewport.requestLoad(file)
    }

    Platform.FileDialog {
        id: relinkFileDialog
        title: rootWindow.tr("Select original layer source")
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["Vector files (*.shp *.gpkg *.geojson *.json *.dxf)", "All files (*)"]
        onAccepted: sourcePathField.text = mapViewport.localPathFromUrl(file)
    }
}
