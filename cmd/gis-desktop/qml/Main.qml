pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Window
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs as QuickDialogs
import Qt.labs.platform as Platform
import QtCore
import GoGIS 1.0

ApplicationWindow {
    id: rootWindow
    visible: true
    width: 1440
    height: 900
    minimumWidth: 960
    minimumHeight: 640
    title: "GoGIS"
    color: "#f4f6f8"
    function syncTransientWindowModels() {
        if (rootWindow.visible && rootWindow.visibility !== Window.Minimized)
            mapViewport.restoreTransientWindowModels();
        else
            mapViewport.releaseTransientWindowModels();
    }
    onVisibleChanged: syncTransientWindowModels()
    onVisibilityChanged: {
        var requestedState = rootWindow.visibility;
        syncTransientWindowModels();
        Qt.callLater(function() {
            console.info("GoGIS window-state=" + requestedState +
                         " current=" + rootWindow.visibility +
                         " size=" + rootWindow.width + "x" + rootWindow.height +
                         " map=" + mapViewport.width + "x" + mapViewport.height +
                         " layers=" + layerModel.count);
        });
    }
    property bool vertexEditMode: false
    property bool compactToolbar: width < 1360
    property string language: appLanguagePreference === "system" ? systemInterfaceLanguage() : appLanguagePreference
    readonly property string appLanguagePreference: typeof appLanguage === "undefined" ? "system" : appLanguage
    property string versionText: typeof appVersion === "undefined" ? "0.1.0-dev" : appVersion
    property string runtimeText: typeof appRuntime === "undefined" ? "Go runtime" : appRuntime
    property string buildTargetText: typeof appBuildTarget === "undefined" ? "desktop" : appBuildTarget
    // Status updates can arrive every render tick while a large layer loads.
    // Keep the language-specific prefix order instead of rebuilding and
    // sorting it for every transient status message.
    property var statusPrefixCache: ({})
    property var statusDictionaryCache: ({})
    onVertexEditModeChanged: {
        if (vertexEditMode && mapCanvas.visible)
            mapViewport.vertexHandleGenerationSeen = -1;
        else
            mapViewport.vertexHandleModel.rows = [];
    }
    function systemInterfaceLanguage() {
        var localeLanguage = String(Qt.locale().name).toLowerCase().split(/[-_]/)[0]
        if (localeLanguage === "ko") return "ko"
        if (localeLanguage === "ja" || localeLanguage === "jp") return "jp"
        return "en"
    }
    Settings {
        id: userSettings
        objectName: "applicationSettingsStore"
        category: "preferences"
        property string interfaceLanguage: "system"
        property int shapefileIndexThreshold: 10000
        property int visibleFeatureLimit: 500000
        property int visiblePayloadLimitMiB: 512
    }
    Binding {
        target: rootWindow
        property: "language"
        value: userSettings.interfaceLanguage === "system"
            ? rootWindow.systemInterfaceLanguage()
            : userSettings.interfaceLanguage
        when: rootWindow.appLanguagePreference === "system"
    }
    property var translations: ({
        en: ({"Add vector files": "Add vector files", "Open workspace": "Open workspace", "Attributes": "Attributes", "Edit vertices": "Edit vertices", "Finish vertex edit": "Finish vertex edit", "Save GeoPackage": "Save GeoPackage", "Save workspace": "Save workspace", "About GoGIS": "About GoGIS", "Layers": "Layers", "No layers yet": "No layers yet", "Add vector files or open a workspace to begin.": "Add vector files or open a workspace to begin.", "Drag to pan · Scroll to zoom · Click a feature to inspect": "Drag to pan · Scroll to zoom · Click a feature to inspect", "General": "General", "Data source": "Data source", "Symbology": "Symbology", "Labels and expressions": "Labels and expressions", "Layer properties": "Layer properties", "Version": "Version", "Build": "Build", "Runtime": "Runtime", "License": "License", "Close": "Close", "Layer": "Layer", "Layer visible": "Layer visible", "Browse…": "Browse…", "Layer in source": "Layer in source", "Shapefile encoding": "Shapefile encoding", "Point color": "Point color", "Point size (mm)": "Point size (mm)", "Line color": "Line color", "Line width (mm)": "Line width (mm)", "Polygon color": "Polygon color", "Fill opacity (0–1)": "Fill opacity (0–1)", "Show labels": "Show labels", "Label field / template": "Label field / template", "Placement": "Placement", "Point label position": "Point label position", "Point label offset (mm)": "Point label offset (mm)", "Rotation field (optional)": "Rotation field (optional)", "Text height (mm)": "Text height (mm)", "Minimum scale denominator": "Minimum scale denominator", "Maximum scale denominator": "Maximum scale denominator", "Display rule — return true to show this feature's label": "Display rule — return true to show this feature's label", "Label text — return string, number, or nil": "Label text — return string, number, or nil", "Insert label example": "Insert label example", "Insert rule example": "Insert rule example", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.", "Layer settings": "Layer settings", "Source path": "Source path", "Data properties": "Data properties"}),
        ko: ({"Add vector files": "벡터 파일 추가", "Open workspace": "작업공간 열기", "Attributes": "속성 테이블", "Edit vertices": "정점 편집", "Finish vertex edit": "정점 편집 종료", "Save GeoPackage": "GeoPackage 저장", "Save workspace": "작업공간 저장", "About GoGIS": "GoGIS 정보", "Layers": "레이어", "No layers yet": "레이어가 없습니다", "Add vector files or open a workspace to begin.": "벡터 파일을 추가하거나 작업공간을 열어 시작하세요.", "Drag to pan · Scroll to zoom · Click a feature to inspect": "드래그: 이동 · 휠: 확대/축소 · 피처 클릭: 정보 확인", "General": "일반", "Data source": "데이터 원본", "Symbology": "심볼로지", "Labels and expressions": "레이블 및 표현식", "Layer properties": "레이어 속성", "Version": "버전", "Build": "빌드", "Runtime": "실행 환경", "License": "라이선스", "Close": "닫기", "Layer": "레이어", "Layer visible": "레이어 표시", "Browse…": "찾아보기…", "Layer in source": "원본 내부 레이어", "Shapefile encoding": "Shapefile 인코딩", "Point color": "점 색상", "Point size (mm)": "점 크기 (mm)", "Line color": "선 색상", "Line width (mm)": "선 두께 (mm)", "Polygon color": "폴리곤 색상", "Fill opacity (0–1)": "채우기 불투명도 (0–1)", "Show labels": "레이블 표시", "Label field / template": "레이블 필드 / 템플릿", "Placement": "배치", "Point label position": "점 레이블 위치", "Point label offset (mm)": "점에서 레이블 간격 (mm)", "Rotation field (optional)": "회전 필드 (선택)", "Text height (mm)": "글자 높이 (mm)", "Minimum scale denominator": "최소 축척 분모", "Maximum scale denominator": "최대 축척 분모", "Display rule — return true to show this feature's label": "표시 규칙 — 레이블 표시 시 true 반환", "Label text — return string, number, or nil": "레이블 문자열 — 문자열, 숫자 또는 nil 반환", "Insert label example": "레이블 예제 삽입", "Insert rule example": "규칙 예제 삽입", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "스크립트는 피처마다 실행됩니다. 읽기 전용 `feature` 테이블로 속성에 접근합니다. 공백이 있는 필드는 feature[\"필드 이름\"] 형식을 사용하세요.", "Layer settings": "레이어 설정", "Source path": "원본 경로", "Data properties": "데이터 속성"}),
        jp: ({"Add vector files": "ベクターファイルを追加", "Open workspace": "ワークスペースを開く", "Attributes": "属性テーブル", "Edit vertices": "頂点を編集", "Finish vertex edit": "頂点編集を終了", "Save GeoPackage": "GeoPackageを保存", "Save workspace": "ワークスペースを保存", "About GoGIS": "GoGISについて", "Layers": "レイヤー", "No layers yet": "レイヤーがありません", "Add vector files or open a workspace to begin.": "ベクターファイルを追加するか、ワークスペースを開いてください。", "Drag to pan · Scroll to zoom · Click a feature to inspect": "ドラッグ: 移動 · ホイール: 拡大/縮小 · 地物をクリック: 情報表示", "General": "一般", "Data source": "データソース", "Symbology": "シンボロジ", "Labels and expressions": "ラベルと式", "Layer properties": "レイヤーのプロパティ", "Version": "バージョン", "Build": "ビルド", "Runtime": "ランタイム", "License": "ライセンス", "Close": "閉じる", "Layer": "レイヤー", "Layer visible": "レイヤーを表示", "Browse…": "参照…", "Layer in source": "ソース内レイヤー", "Shapefile encoding": "Shapefileの文字コード", "Point color": "ポイント色", "Point size (mm)": "ポイントサイズ (mm)", "Line color": "ライン色", "Line width (mm)": "ライン幅 (mm)", "Polygon color": "ポリゴン色", "Fill opacity (0–1)": "塗りの不透明度 (0–1)", "Show labels": "ラベルを表示", "Label field / template": "ラベルフィールド / テンプレート", "Placement": "配置", "Point label position": "ポイントラベル位置", "Point label offset (mm)": "ポイントからのラベル間隔 (mm)", "Rotation field (optional)": "回転フィールド (任意)", "Text height (mm)": "文字の高さ (mm)", "Minimum scale denominator": "最小縮尺分母", "Maximum scale denominator": "最大縮尺分母", "Display rule — return true to show this feature's label": "表示ルール — ラベル表示時にtrueを返す", "Label text — return string, number, or nil": "ラベル文字列 — 文字列、数値、またはnilを返す", "Insert label example": "ラベル例を挿入", "Insert rule example": "ルール例を挿入", "Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.": "スクリプトは地物ごとに実行されます。読み取り専用の`feature`テーブルから属性を参照できます。空白を含むフィールド名はfeature[\"フィールド名\"]を使用します。", "Layer settings": "レイヤー設定", "Source path": "ソースパス", "Data properties": "データ属性"})
    })
    property var translationOverrides: ({
        en: ({"Desktop GIS": "Desktop GIS", "Layer properties": "Layer properties", "Select original layer source": "Select original layer source", "Add vector files as layers": "Add vector files as layers", "Save GoGIS workspace": "Save GoGIS workspace", "Open GoGIS workspace": "Open GoGIS workspace", "Drop SHP, GeoPackage, or GeoJSON": "Drop SHP, GeoPackage, or GeoJSON", "Display name": "Display name", "Original source path": "Original source path", "Internal layer name": "Internal layer name", "Auto encoding": "Auto encoding", "Selected feature": "Selected feature", "Feature name": "Feature name", "Save": "Save", "Cancel": "Cancel", "Previous": "Previous", "Next": "Next", "No attribute records in this layer": "No attribute records in this layer", "X coordinate": "X coordinate", "Y coordinate": "Y coordinate", "Go": "Go", "Cancel loading/render": "Cancel loading/render", "Layer settings": "Layer settings", "Source path": "Source path", "Data properties": "Data properties", "Point color": "Point color", "Point size (mm)": "Point size (mm)", "Line color": "Line color", "Line width (mm)": "Line width (mm)", "Polygon color": "Polygon color", "Fill opacity (0–1)": "Fill opacity (0–1)", "Show labels": "Show labels", "Label field / template": "Label field / template", "Placement": "Placement", "Rotation field (optional)": "Rotation field (optional)", "Text height (mm)": "Text height (mm)", "Minimum scale denominator": "Minimum scale denominator", "Maximum scale denominator": "Maximum scale denominator"}),
        ko: ({"Desktop GIS": "데스크톱 GIS", "Layer properties": "레이어 속성", "Select original layer source": "레이어 원본 선택", "Add vector files as layers": "벡터 파일을 레이어로 추가", "Save GoGIS workspace": "GoGIS 작업공간 저장", "Open GoGIS workspace": "GoGIS 작업공간 열기", "Drop SHP, GeoPackage, or GeoJSON": "SHP, GeoPackage 또는 GeoJSON 파일을 놓으세요", "Display name": "표시 이름", "Original source path": "원본 경로", "Internal layer name": "내부 레이어 이름", "Auto encoding": "인코딩 자동 감지", "Selected feature": "선택한 피처", "Feature name": "피처 이름", "Save": "저장", "Cancel": "취소", "Previous": "이전", "Next": "다음", "No attribute records in this layer": "이 레이어에 속성 레코드가 없습니다", "X coordinate": "X 좌표", "Y coordinate": "Y 좌표", "Go": "이동", "Cancel loading/render": "불러오기/렌더링 취소", "Layer settings": "레이어 설정", "Source path": "원본 경로", "Data properties": "데이터 속성", "Point color": "점 색상", "Point size (mm)": "점 크기 (mm)", "Line color": "선 색상", "Line width (mm)": "선 두께 (mm)", "Polygon color": "폴리곤 색상", "Fill opacity (0–1)": "채우기 불투명도 (0–1)", "Show labels": "레이블 표시", "Label field / template": "레이블 필드 / 템플릿", "Placement": "배치", "Rotation field (optional)": "회전 필드 (선택)", "Text height (mm)": "글자 높이 (mm)", "Minimum scale denominator": "최소 축척 분모", "Maximum scale denominator": "최대 축척 분모"}),
        jp: ({"Desktop GIS": "デスクトップGIS", "Layer properties": "レイヤーのプロパティ", "Select original layer source": "レイヤーソースを選択", "Add vector files as layers": "ベクターファイルをレイヤーとして追加", "Save GoGIS workspace": "GoGISワークスペースを保存", "Open GoGIS workspace": "GoGISワークスペースを開く", "Drop SHP, GeoPackage, or GeoJSON": "SHP、GeoPackage、GeoJSONをドロップ", "Display name": "表示名", "Original source path": "元のソースパス", "Internal layer name": "内部レイヤー名", "Auto encoding": "文字コードを自動判定", "Selected feature": "選択地物", "Feature name": "地物名", "Save": "保存", "Cancel": "キャンセル", "Previous": "前へ", "Next": "次へ", "No attribute records in this layer": "このレイヤーに属性レコードはありません", "X coordinate": "X座標", "Y coordinate": "Y座標", "Go": "移動", "Cancel loading/render": "読み込み/描画をキャンセル", "Layer settings": "レイヤー設定", "Source path": "ソースパス", "Data properties": "データ属性", "Point color": "ポイント色", "Point size (mm)": "ポイントサイズ (mm)", "Line color": "ライン色", "Line width (mm)": "ライン幅 (mm)", "Polygon color": "ポリゴン色", "Fill opacity (0–1)": "塗りの不透明度 (0–1)", "Show labels": "ラベルを表示", "Label field / template": "ラベルフィールド / テンプレート", "Placement": "配置", "Rotation field (optional)": "回転フィールド (任意)", "Text height (mm)": "文字の高さ (mm)", "Minimum scale denominator": "最小縮尺分母", "Maximum scale denominator": "最大縮尺分母"})
    })

    property var luaTranslations: ({
        en: ({
            "Lua editor": "Lua editor", "Display filter": "Display filter", "Label rule": "Label rule", "Label text": "Label text", "Examples": "Examples", "Lua helpers": "Lua helpers",
            "Lua label editor": "Lua label editor",
            "Export project layers to DXF": "Export project layers to DXF",
            "Feature display filter — return true to draw this feature": "Feature display filter — return true to draw this feature",
            "Insert field into display filter…": "Insert field into display filter…",
            "Feature display filter hides features from the map only; source data stays unchanged.": "Feature display filter hides features from the map only; source data stays unchanged.",
            "Open Lua editor and examples…": "Open Lua editor and examples…",
            "Lua field access hint": "feature.FIELD reads an attribute; use feature[\"field name\"] when a field contains spaces. Types: %1",
            "Lua API help": "These pure helpers work in label text, display rules, and project Lua scripts. Rules return boolean; label text returns string, number, or nil.",
            "Insert field into rule…": "Insert field into rule…",
            "Insert field into label…": "Insert field into label…",
            "Insert LIKE example": "Insert LIKE example",
            "Insert concatenation example": "Insert concatenation example",
            "Insert number format example": "Insert number format example",
            "Center": "Center", "Center + rotation": "Center + rotation", "Free angle": "Free angle",
            "Lua field types": "Lua field types", "Open the attribute table to inspect the layer schema.": "Open the attribute table to inspect the layer schema."
        }),
        ko: ({
            "Lua editor": "Lua 편집기", "Display filter": "표시 필터", "Label rule": "레이블 규칙", "Label text": "레이블 문자", "Examples": "예제", "Lua helpers": "Lua 함수 도움말",
            "Lua label editor": "Lua 레이블 편집기",
            "Export project layers to DXF": "프로젝트 레이어를 DXF로 내보내기",
            "Feature display filter — return true to draw this feature": "피처 표시 필터 — 그릴 피처는 true 반환",
            "Insert field into display filter…": "표시 필터에 필드 삽입…",
            "Feature display filter hides features from the map only; source data stays unchanged.": "피처 표시 필터는 지도에서만 숨기며 원본 데이터는 바뀌지 않습니다.",
            "Open Lua editor and examples…": "Lua 편집기 및 예제 열기…",
            "Lua field access hint": "feature.FIELD로 속성을 읽습니다. 필드명에 공백이 있으면 feature[\"필드 이름\"]을 사용하세요. 필드 형식: %1",
            "Lua API help": "아래 보조 함수는 레이블·표시 규칙과 프로젝트 Lua 스크립트에서 사용할 수 있습니다. 규칙은 boolean, 레이블 식은 string/number/nil을 반환합니다.",
            "Insert field into rule…": "규칙에 필드 삽입…",
            "Insert field into label…": "레이블에 필드 삽입…",
            "Insert LIKE example": "LIKE(포함) 예제 삽입",
            "Insert concatenation example": "문자열 합치기 예제 삽입",
            "Insert number format example": "숫자 포맷 예제 삽입",
            "Center": "중앙", "Center + rotation": "중앙 + 회전", "Free angle": "자유 각도",
            "Lua field types": "Lua 필드 형식", "Open the attribute table to inspect the layer schema.": "레이어 필드를 확인하려면 속성 테이블을 여세요."
        }),
        jp: ({
            "Lua editor": "Luaエディター", "Display filter": "表示フィルター", "Label rule": "ラベルルール", "Label text": "ラベル文字", "Examples": "例", "Lua helpers": "Lua関数のヘルプ",
            "Lua label editor": "Luaラベルエディター",
            "Export project layers to DXF": "プロジェクトのレイヤーをDXFにエクスポート",
            "Feature display filter — return true to draw this feature": "地物表示フィルター — 描画する地物はtrueを返す",
            "Insert field into display filter…": "表示フィルターにフィールドを挿入…",
            "Feature display filter hides features from the map only; source data stays unchanged.": "地物表示フィルターは地図上で非表示にするだけで、元データは変更しません。",
            "Open Lua editor and examples…": "Luaエディターと例を開く…",
            "Lua field access hint": "feature.FIELDで属性を読み取ります。空白を含むフィールド名にはfeature[\"フィールド名\"]を使用します。型: %1",
            "Lua API help": "以下の補助関数はラベル・表示ルールとプロジェクトLuaスクリプトで使用できます。ルールはboolean、ラベル式はstring/number/nilを返します。",
            "Insert field into rule…": "ルールにフィールドを挿入…",
            "Insert field into label…": "ラベルにフィールドを挿入…",
            "Insert LIKE example": "LIKE例を挿入",
            "Insert concatenation example": "文字列連結例を挿入",
            "Insert number format example": "数値フォーマット例を挿入",
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

    property var attributeTranslations: ({
        en: ({"Layer": "Layer", "Feature": "Feature", "Editable": "Editable", "Yes": "Yes", "Read-only": "Read-only", "Page": "Page"}),
        ko: ({"Layer": "레이어", "Feature": "피처", "Editable": "편집 가능", "Yes": "예", "Read-only": "읽기 전용", "Page": "페이지"}),
        jp: ({"Layer": "レイヤー", "Feature": "地物", "Editable": "編集可能", "Yes": "はい", "Read-only": "読み取り専用", "Page": "ページ"})
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
        var attributeDictionary = attributeTranslations[language] || attributeTranslations.en;
        if (attributeDictionary[key] !== undefined)
            return attributeDictionary[key];
        var viewDictionary = viewTranslations[language] || viewTranslations.en;
        if (viewDictionary[key] !== undefined)
            return viewDictionary[key];
        var dictionary = translations[language] || translations.en;
        return dictionary[key] || translations.en[key] || key;
    }

    function localizedStatus(raw) {
        var status = String(raw || "");
        var renderReady = /^Render ready: (\d+)\/(\d+) chunks contain geometry$/.exec(status);
        if (renderReady) {
            if (language === "ko") return "타일 렌더링 완료 · " + renderReady[1] + "/" + renderReady[2] + "개 청크에 도형 포함";
            if (language === "jp") return "タイル描画完了 · " + renderReady[2] + " 件中 " + renderReady[1] + " 件に地物あり";
            return status;
        }
        if (status.indexOf("Reprojecting project to ") === 0) {
            var targetCRS = status.substring("Reprojecting project to ".length);
            return language === "ko" ? "프로젝트 좌표계로 변환 중: " + targetCRS : language === "jp" ? "プロジェクト座標系に変換中: " + targetCRS : status;
        }
        if (status.indexOf("Project CRS set to ") === 0) {
            var appliedCRS = status.substring("Project CRS set to ".length);
            return language === "ko" ? "프로젝트 좌표계 적용 완료: " + appliedCRS : language === "jp" ? "プロジェクト座標系を適用しました: " + appliedCRS : status;
        }
        if (status.indexOf("Project CRS change failed: ") === 0) {
            var crsError = status.substring("Project CRS change failed: ".length);
            return language === "ko" ? "프로젝트 좌표계 변경 실패: " + crsError : language === "jp" ? "プロジェクト座標系の変更に失敗しました: " + crsError : status;
        }
        if (status.indexOf("Showing ") === 0 && status.indexOf(" labels in the current view; zoom in for more") > 0) {
            var visibleLabelCount = status.substring("Showing ".length).split(" labels")[0];
            return language === "ko" ? "현재 화면 레이블 " + visibleLabelCount + "개 표시 중 · 더 보려면 확대하세요" : language === "jp" ? "現在の画面にラベル " + visibleLabelCount + " 件を表示中 · 拡大すると増えます" : status;
        }
        if (status === "Removing layer")
            return language === "ko" ? "레이어 제거 중" : language === "jp" ? "レイヤーを除去中" : status;
        if (status.indexOf("Layer removed: ") === 0)
            return (language === "ko" ? "레이어 제거 완료: " : language === "jp" ? "レイヤーを除去しました: " : "Layer removed: ") + status.substring("Layer removed: ".length);
        if (status.indexOf("Remove layer failed: ") === 0)
            return (language === "ko" ? "레이어 제거 실패: " : language === "jp" ? "レイヤーの除去に失敗しました: " : "Remove layer failed: ") + status.substring("Remove layer failed: ".length);
        var dictionary = statusDictionaryCache[language];
        if (dictionary === undefined) dictionary = {
            en: {"Loading": "Loading", "Loading: checking feature count": "Checking feature count", "Preview displayed; loading full data": "Preview displayed; loading full data", "Layer settings applied": "Layer settings applied", "Loading cancelled": "Loading cancelled", "Render cancelled": "Render cancelled", "Dataset size unavailable; opened read-only to limit memory": "Dataset size unavailable; opened read-only to limit memory", "Render incomplete; zoom in and try again: ": "Render incomplete; zoom in and try again: ", "Open failed: ": "Open failed: ", "Save failed: ": "Save failed: ", "Workspace load failed: ": "Workspace load failed: ", "Layer settings failed: ": "Layer settings failed: ", "Render stopped: ": "Render stopped: ", "Render error: ": "Render error: ", "Render skipped: ": "Render skipped: ", "Loading ": "Loading ", "Saving ": "Saving ", "Saved ": "Saved ", "Dataset has at least ": "Dataset has at least "},
            ko: {"Loading": "불러오는 중", "Loading: checking feature count": "피처 개수 확인 중", "Preview displayed; loading full data": "미리보기 표시됨 · 전체 데이터 불러오는 중", "Layer settings applied": "레이어 설정 적용 완료", "Loading cancelled": "불러오기 취소됨", "Render cancelled": "렌더링 취소됨", "Dataset size unavailable; opened read-only to limit memory": "데이터 크기를 알 수 없어 메모리 보호를 위해 읽기 전용으로 열었습니다", "Render incomplete; zoom in and try again: ": "일부 렌더링을 완료하지 못했습니다. 확대 후 다시 시도하세요: ", "Open failed: ": "열기 실패: ", "Save failed: ": "저장 실패: ", "Workspace load failed: ": "작업공간 열기 실패: ", "Workspace save failed: ": "작업공간 저장 실패: ", "Layer settings failed: ": "레이어 설정 실패: ", "Layer reload failed: ": "레이어 다시 열기 실패: ", "Attribute page failed: ": "속성 페이지 표시 실패: ", "Attribute page exceeds the 16 MiB display payload limit": "속성 페이지가 16 MiB 표시 한도를 초과했습니다", "Render stopped: ": "렌더링 중단: ", "Render error: ": "렌더링 오류: ", "Render skipped: ": "렌더링 생략: ", "Label display skipped: ": "레이블 표시 생략: ", "Loading ": "불러오는 중: ", "Saving ": "저장 중: ", "Saved ": "저장 완료: ", "Workspace saved ": "작업공간 저장 완료: ", "Workspace loaded ": "작업공간 열기 완료: ", "Loading workspace ": "작업공간 불러오는 중: ", "Add files requires the native GDAL build": "파일을 추가하려면 GDAL 지원 데스크톱 빌드가 필요합니다", "Reopening layer source with selected encoding": "선택한 인코딩으로 레이어 원본 다시 여는 중", "Layer source reloaded": "레이어 원본 다시 열기 완료", "Vertex edit failed: ": "정점 편집 실패: ", "Vertex moved": "정점 이동 완료", "No visible layers": "표시 중인 레이어가 없습니다", "No vector files selected": "벡터 파일을 선택하지 않았습니다", "A file load is already in progress": "파일을 불러오는 중입니다", "Selected source is already loaded": "선택한 원본이 이미 열려 있습니다", "Cannot add files until the read-only source is ready": "읽기 전용 원본이 준비될 때까지 파일을 추가할 수 없습니다", "Open cancelled": "열기를 취소했습니다", "Save cancelled": "저장을 취소했습니다", "Dataset has at least ": "피처가 최소 "},
            jp: {"Loading": "読み込み中", "Loading: checking feature count": "地物数を確認中", "Preview displayed; loading full data": "プレビューを表示 · 全データを読み込み中", "Layer settings applied": "レイヤー設定を適用しました", "Loading cancelled": "読み込みをキャンセルしました", "Render cancelled": "描画をキャンセルしました", "Dataset size unavailable; opened read-only to limit memory": "メモリ保護のため読み取り専用で開きました（データ件数不明）", "Render incomplete; zoom in and try again: ": "一部を描画できません。拡大して再試行してください: ", "Open failed: ": "開けませんでした: ", "Save failed: ": "保存できませんでした: ", "Workspace load failed: ": "ワークスペースを開けませんでした: ", "Workspace save failed: ": "ワークスペースを保存できませんでした: ", "Layer settings failed: ": "レイヤー設定に失敗しました: ", "Layer reload failed: ": "レイヤーを再読み込みできませんでした: ", "Attribute page failed: ": "属性ページを表示できませんでした: ", "Attribute page exceeds the 16 MiB display payload limit": "属性ページが16 MiBの表示上限を超えました", "Render stopped: ": "描画を停止しました: ", "Render error: ": "描画エラー: ", "Render skipped: ": "描画を省略しました: ", "Label display skipped: ": "ラベル表示を省略しました: ", "Loading ": "読み込み中: ", "Saving ": "保存中: ", "Saved ": "保存しました: ", "Workspace saved ": "ワークスペースを保存しました: ", "Workspace loaded ": "ワークスペースを開きました: ", "Workspace loaded; relink unavailable layers: ": "ワークスペースを開きました。再リンクが必要なレイヤー: ", "Loading workspace ": "ワークスペースを読み込み中: ", "Add files requires the native GDAL build": "ファイル追加にはGDAL対応デスクトップビルドが必要です", "Reopening layer source with selected encoding": "選択した文字コードでレイヤーソースを再読み込み中", "Layer source reloaded": "レイヤーソースを再読み込みしました", "Vertex edit failed: ": "頂点編集に失敗しました: ", "Vertex moved": "頂点を移動しました", "No visible layers": "表示中のレイヤーがありません", "No vector files selected": "ベクターファイルが選択されていません", "A file load is already in progress": "ファイルを読み込み中です", "Selected source is already loaded": "選択したソースは既に開いています", "Cannot add files until the read-only source is ready": "読み取り専用ソースの準備完了後に追加できます", "Open cancelled": "開く操作をキャンセルしました", "Save cancelled": "保存をキャンセルしました", "Dataset has at least ": "地物数が少なくとも "}
        }[language] || {};
        if (statusDictionaryCache[language] === undefined)
            statusDictionaryCache[language] = dictionary;
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
        var prefixes = statusPrefixCache[language];
        if (prefixes === undefined) {
            prefixes = Object.keys(dictionary).sort(function(a, b) { return b.length - a.length; });
            statusPrefixCache[language] = prefixes;
        }
        for (var i = 0; i < prefixes.length; ++i) {
            if (status.indexOf(prefixes[i]) === 0) return dictionary[prefixes[i]] + status.substring(prefixes[i].length);
        }
        return status;
    }

    function statusColor(raw) {
        var status = String(raw || "").toLowerCase();
        if (status.indexOf("failed") >= 0 || status.indexOf("error") >= 0 || status.indexOf("invalid") >= 0 || status.indexOf("exceeds") >= 0 || status.indexOf("incomplete") >= 0 || status.indexOf("stopped") >= 0 || status.indexOf("requires") >= 0) return "#b42318";
        if (status.indexOf("cancel") >= 0 || status.indexOf("unavailable") >= 0 || status.indexOf("cannot") >= 0 || status.indexOf("skipped") >= 0 || status.indexOf("relink") >= 0) return "#9a6700";
        if (status.indexOf("read-only") >= 0 || status.indexOf("readonly") >= 0 || status.indexOf("loading") >= 0 || status.indexOf("importing") >= 0 || status.indexOf("saving") >= 0 || status.indexOf("running ") >= 0 || status.indexOf("spatial") >= 0 || status.indexOf("rendering") >= 0 || status.indexOf("removing") >= 0 || status.indexOf("preview") >= 0 || status.indexOf("checking") >= 0 || status.indexOf("reprojecting") >= 0) return "#175cd3";
        if (status.indexOf("saved") >= 0 || status.indexOf("applied") >= 0 || status.indexOf("loaded") >= 0 || status.indexOf("removed") >= 0 || status.indexOf("ready") >= 0 || status.indexOf("project crs set") >= 0) return "#2e7d32";
        return "#65717d";
    }

    function statusIsBusy(raw) {
        var status = String(raw || "").toLowerCase();
        if (status.indexOf("csv preview:") === 0 || status.indexOf("csv imported:") === 0 || status.indexOf("validation complete:") === 0 || status.indexOf("join complete:") === 0)
            return false;
        // Terminal messages can contain the same operation word as their
        // running state (for example "spatial operation cancelled").
        if (status.indexOf("cancel") >= 0 || status.indexOf("failed") >= 0 ||
            status.indexOf("error") >= 0 || status.indexOf("stopped") >= 0 ||
            status.indexOf("saved") >= 0 || status.indexOf("applied") >= 0 ||
            status.indexOf("added") >= 0 || status.indexOf("deleted") >= 0 ||
            status.indexOf("removed") >= 0 || status.indexOf("exported") >= 0 ||
            status.indexOf("ready") >= 0)
            return false;
        return status.indexOf("loading") >= 0 || status.indexOf("importing") >= 0 || status.indexOf("saving") >= 0 || status.indexOf("running ") >= 0 || status.indexOf("spatial") >= 0 || status.indexOf("rendering") >= 0 || status.indexOf("preview") >= 0 || status.indexOf("removing") >= 0 || status.indexOf("reprojecting") >= 0;
    }

    function renderProgressSnapshot(raw) {
        var match = /Loading(?: approximate overview)?\s+\(?([0-9]+)\/([0-9]+)/i.exec(String(raw || ""));
        if (!match || Number(match[2]) <= 0)
            return {known: false, value: 0};
        return {known: true, value: Math.max(0, Math.min(100, Number(match[1]) * 100 / Number(match[2])))};
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
        property var lineStarts: [0]
        implicitHeight: 144
        clip: true

        function updateLineStarts() {
            var starts = [0];
            for (var index = 0; index < sourceArea.text.length; ++index) {
                if (sourceArea.text[index] === "\n") starts.push(index + 1);
            }
            lineStarts = starts;
        }

        function visibleLines() {
            var starts = lineStarts;
            var top = sourceFlickable.contentY;
            var bottom = top + sourceFlickable.height;
            var low = 0;
            var high = starts.length;
            while (low < high) {
                var middle = Math.floor((low + high) / 2);
                if (sourceArea.positionToRectangle(starts[middle]).y < top)
                    low = middle + 1;
                else
                    high = middle;
            }
            var result = [];
            for (var line = Math.max(0, low - 1); line < starts.length; ++line) {
                var rectangle = sourceArea.positionToRectangle(starts[line]);
                if (rectangle.y > bottom) break;
                result.push({number: line + 1, y: rectangle.y - top, height: rectangle.height});
            }
            return result;
        }

        Rectangle {
            anchors.fill: parent
            z: -1
            color: "#ffffff"
            border.color: sourceArea.activeFocus ? "#4682b4" : "#cfd6dd"
            radius: 3
        }

        function insert(position, value) {
            sourceArea.insert(position, value);
        }
        Flickable {
            id: sourceFlickable
            objectName: "luaSourceFlickable"
            anchors.fill: parent
            clip: true
            contentWidth: width
            contentHeight: sourceArea.height
            boundsBehavior: Flickable.StopAtBounds
            TextArea.flickable: TextArea {
                id: sourceArea
                objectName: "luaSourceArea"
                width: sourceFlickable.width
                height: Math.max(sourceFlickable.height, implicitHeight)
                leftPadding: 66
                rightPadding: 8
                topPadding: 8
                bottomPadding: 8
                wrapMode: TextEdit.WrapAnywhere
                textFormat: TextEdit.PlainText
                selectByMouse: true
                font.family: Qt.platform.os === "osx" ? "Menlo" : Qt.platform.os === "windows" ? "Consolas" : "DejaVu Sans Mono"
                font.pixelSize: 14
                color: "#263238"
                selectedTextColor: "#ffffff"
                selectionColor: "#557aa6d6"
                background: null
                cursorDelegate: Rectangle {
                    width: 1
                    color: "#263238"
                }
                onTextChanged: luaCodeEditor.updateLineStarts()
            }
        }
        Item {
            objectName: "luaLineNumberGutter"
            x: 0
            y: 0
            width: 50
            height: parent.height
            clip: true
            z: 1
            Repeater {
                model: luaCodeEditor.visibleLines()
                delegate: Text {
                    required property var modelData
                    objectName: "luaLineNumber" + modelData.number
                    x: 8
                    y: modelData.y
                    width: 40
                    height: modelData.height
                    text: String(modelData.number)
                    horizontalAlignment: Text.AlignRight
                    color: "#8a949e"
                    font: sourceArea.font
                }
            }
        }
        Rectangle {
            x: 56
            y: 0
            width: 1
            height: parent.height
            color: "#e2e6ea"
        }
        LuaHighlighter {
            objectName: "luaHighlighter"
            visible: false
            property var textDocument: sourceArea.textDocument
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
                objectName: "openPostGISTablesButton"
                text: rootWindow.language === "ko" ? "PostGIS 테이블" : rootWindow.language === "jp" ? "PostGISテーブル" : "PostGIS tables"
                onClicked: postGISTableDialog.open()
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
                objectName: "projectCRSButton"
                visible: !rootWindow.compactToolbar
                text: rootWindow.language === "ko" ? "프로젝트 좌표계" : rootWindow.language === "jp" ? "プロジェクト座標系" : "Project CRS"
                onClicked: projectCRSDialog.open()
            }
            Button {
                objectName: "toggleVertexEditButton"
                visible: !rootWindow.compactToolbar
                text: rootWindow.vertexEditMode ? "Finish vertex edit" : "Edit vertices"
                enabled: layerModel.count > 0 && mapCanvas.visible
                onClicked: rootWindow.vertexEditMode = !rootWindow.vertexEditMode
            }
            Button {
                id: projectActionsButtonControl
                objectName: "projectActionsButton"
                text: rootWindow.language === "ko" ? "저장·내보내기" : rootWindow.language === "jp" ? "保存・エクスポート" : "Save / Export"
                onClicked: projectActionsMenu.popup(projectActionsButtonControl, 0, projectActionsButtonControl.height)
            }
            Button {
                id: moreActionsButton
                objectName: "moreActionsButton"
                visible: rootWindow.compactToolbar
                text: rootWindow.language === "ko" ? "더보기" : rootWindow.language === "jp" ? "その他" : "More"
                onClicked: moreActionsMenu.popup(moreActionsButton, 0, moreActionsButton.height)
            }
            Button {
                objectName: "aboutButton"
                visible: !rootWindow.compactToolbar
                text: rootWindow.tr("About GoGIS")
                onClicked: aboutDialog.open()
            }
            Button {
                objectName: "applicationSettingsButton"
                visible: !rootWindow.compactToolbar
                text: "⚙"
                Accessible.name: rootWindow.language === "ko" ? "설정" : rootWindow.language === "jp" ? "設定" : "Settings"
                ToolTip.visible: hovered
                ToolTip.text: Accessible.name
                onClicked: applicationSettingsDialog.open()
            }
        }
    }

    Menu {
        id: moreActionsMenu
        objectName: "moreActionsMenu"
        MenuItem {
            objectName: "moreProjectCRSAction"
            text: rootWindow.language === "ko" ? "프로젝트 좌표계" : rootWindow.language === "jp" ? "プロジェクト座標系" : "Project CRS"
            onTriggered: projectCRSDialog.open()
        }
        MenuItem {
            objectName: "moreVertexEditAction"
            text: rootWindow.vertexEditMode ? "Finish vertex edit" : "Edit vertices"
            enabled: layerModel.count > 0 && mapCanvas.visible
            onTriggered: rootWindow.vertexEditMode = !rootWindow.vertexEditMode
        }
        MenuSeparator {}
        MenuItem {
            objectName: "moreAboutAction"
            text: rootWindow.tr("About GoGIS")
            onTriggered: aboutDialog.open()
        }
        MenuItem {
            objectName: "moreSettingsAction"
            text: rootWindow.language === "ko" ? "설정" : rootWindow.language === "jp" ? "設定" : "Settings"
            onTriggered: applicationSettingsDialog.open()
        }
    }

    Menu {
        id: projectActionsMenu
        objectName: "projectActionsMenu"
        MenuItem {
            objectName: "saveGeoPackageAction"
            text: rootWindow.tr("Save GeoPackage")
            enabled: layerModel.count > 0
            onTriggered: saveDialog.open()
        }
        MenuItem {
            objectName: "exportDxfButton"
            text: rootWindow.tr("Export project layers to DXF")
            enabled: layerModel.count > 0
            onTriggered: {
                dxfEncodingDialog.open();
            }
        }
        MenuItem {
            objectName: "spatialOperationAction"
            text: rootWindow.language === "ko" ? "공간 연산…" : rootWindow.language === "jp" ? "空間演算…" : "Spatial operation…"
            enabled: layerModel.count > 0
            onTriggered: spatialOperationDialog.open()
        }
        MenuItem {
            objectName: "importCsvAction"
            text: rootWindow.language === "ko" ? "CSV/TSV 좌표 추가…" : rootWindow.language === "jp" ? "CSV/TSV座標を追加…" : "Import CSV/TSV points…"
            onTriggered: importCsvDialog.open()
        }
        MenuItem {
            objectName: "createEmptyLayerAction"
            text: rootWindow.language === "ko" ? "빈 레이어 만들기…" : "Create empty layer…"
            onTriggered: createLayerDialog.open()
        }
        MenuItem {
            objectName: "workflowAction"
            text: rootWindow.language === "ko" ? "업무 워크플로우…" : "Workflow tools…"
            enabled: layerModel.count > 0
            onTriggered: workflowDialog.open()
        }
        MenuItem {
            objectName: "saveWorkspaceAction"
            text: rootWindow.tr("Save workspace")
            onTriggered: workspaceDialog.open()
        }
    }

    Dialog {
        id: createLayerDialog
        objectName: "createLayerDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "빈 레이어 만들기" : "Create empty layer"
        width: Math.min(450, rootWindow.width - 32)
        property string errorText: ""
        function submitLayer() {
            if (createLayerName.text.trim() === "" || createLayerCRS.text.trim() === "") {
                errorText = rootWindow.language === "ko" ? "레이어 이름과 CRS를 입력하세요." : "Enter a layer name and CRS.";
                return;
            }
            errorText = "";
            accept();
        }
        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 16
            TextField { id: createLayerName; objectName: "createLayerName"; Layout.fillWidth: true; placeholderText: rootWindow.language === "ko" ? "레이어 이름" : "Layer name" }
            ComboBox { id: createLayerGeometry; objectName: "createLayerGeometry"; Layout.fillWidth: true; model: rootWindow.language === "ko" ? ["점", "선", "면"] : ["Point", "Line", "Polygon"] }
            TextField { id: createLayerCRS; objectName: "createLayerCRS"; Layout.fillWidth: true; placeholderText: "CRS (e.g. EPSG:5186)" }
            Label { Layout.fillWidth: true; visible: createLayerDialog.errorText !== ""; color: "#b42318"; text: createLayerDialog.errorText; wrapMode: Text.WordWrap }
        }
        footer: RowLayout {
            Item { Layout.fillWidth: true }
            Button { text: rootWindow.tr("Cancel"); onClicked: createLayerDialog.reject() }
            Button { text: rootWindow.language === "ko" ? "만들기" : "Create"; onClicked: createLayerDialog.submitLayer() }
        }
        onAccepted: {
            var types = ["POINT", "LINESTRING", "POLYGON"];
            mapCanvas.layerSettingsPayload = JSON.stringify({operation: "create-layer", outputName: createLayerName.text.trim(), geometryType: types[createLayerGeometry.currentIndex], crs: createLayerCRS.text.trim().toUpperCase()});
            mapCanvas.layerSettingsGeneration += 1;
        }
    }

    Dialog {
        id: importCsvDialog
        objectName: "importCsvDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "CSV/TSV 좌표 추가" : "Import CSV/TSV points"
        width: Math.min(650, rootWindow.width - 32)
        property string inputPath: ""
        property string previewText: ""
        property string errorText: ""
        onOpened: { previewText = ""; errorText = ""; }
        function submitImport() {
            if (csvPathField.text.trim() === "" || csvLayerField.text.trim() === "" || csvXField.text.trim() === "" || csvYField.text.trim() === "" || csvCrsField.text.trim() === "") {
                errorText = rootWindow.language === "ko" ? "파일·레이어명·X/Y 필드·CRS를 모두 입력하세요." : "Enter a file, layer name, X/Y fields, and CRS.";
                return;
            }
            errorText = "";
            accept();
        }
        Connections {
            target: mapCanvas
            function onRenderStatusChanged() {
                if (mapCanvas.renderStatus.indexOf("CSV preview:") === 0)
                    importCsvDialog.previewText = mapCanvas.renderStatus;
            }
        }
        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 16
            TextArea { id: csvPathField; objectName: "csvPathField"; Layout.fillWidth: true; Layout.preferredHeight: 72; placeholderText: rootWindow.language === "ko" ? "CSV/TSV 경로 (파일마다 한 줄)" : "CSV/TSV paths (one per line)"; text: importCsvDialog.inputPath; wrapMode: TextEdit.NoWrap }
            Button { text: rootWindow.language === "ko" ? "파일 여러 개 선택" : "Choose CSV files"; onClicked: csvOpenDialog.open() }
            TextField { id: csvLayerField; objectName: "csvLayerField"; Layout.fillWidth: true; placeholderText: "Layer name"; text: "csv_points" }
            RowLayout {
                TextField { id: csvXField; objectName: "csvXField"; Layout.fillWidth: true; placeholderText: "X field" }
                TextField { id: csvYField; objectName: "csvYField"; Layout.fillWidth: true; placeholderText: "Y field" }
            }
            RowLayout {
                TextField { id: csvCrsField; objectName: "csvCrsField"; Layout.fillWidth: true; placeholderText: "CRS (EPSG:4326)" }
                TextField { id: csvEncodingField; Layout.fillWidth: true; placeholderText: "Encoding"; text: "UTF-8" }
            }
            TextField { id: csvDelimiterField; Layout.fillWidth: true; placeholderText: "Delimiter (, or tab)"; text: "," }
            TextField { id: csvNumberFields; Layout.fillWidth: true; placeholderText: "Numeric fields (comma-separated, optional)" }
            TextField { id: csvBoolFields; Layout.fillWidth: true; placeholderText: "Boolean fields (comma-separated, optional)" }
            CheckBox { id: csvBuildBoundary; objectName: "csvBuildBoundary"; Layout.fillWidth: true; text: rootWindow.language === "ko" ? "좌표로 볼록 외곽선 계산하고 도형 식별자 부여" : "Calculate convex boundary and assign shape IDs" }
            TextField { id: csvBoundaryGroup; objectName: "csvBoundaryGroup"; Layout.fillWidth: true; visible: csvBuildBoundary.checked; placeholderText: rootWindow.language === "ko" ? "경계 그룹 필드 (비우면 모든 점을 한 경계로)" : "Boundary group field (blank means one boundary)" }
            Label { Layout.fillWidth: true; visible: importCsvDialog.errorText !== ""; color: "#b42318"; text: importCsvDialog.errorText; wrapMode: Text.WordWrap }
            Button {
                text: rootWindow.language === "ko" ? "미리보기" : "Preview"
                onClicked: {
                    var path = csvPathField.text.trim().split(/\r?\n/)[0];
                    if (path === "") return;
                    mapCanvas.layerSettingsPayload = JSON.stringify({operation: "preview-csv", inputPath: path, encoding: csvEncodingField.text.trim() || "UTF-8", delimiter: csvDelimiterField.text});
                    mapCanvas.layerSettingsGeneration += 1;
                }
            }
            ScrollView {
                Layout.fillWidth: true
                Layout.preferredHeight: 105
                TextArea {
                    objectName: "csvPreviewText"
                    readOnly: true
                    wrapMode: TextEdit.NoWrap
                    text: importCsvDialog.previewText !== "" ? importCsvDialog.previewText : (rootWindow.language === "ko" ? "파일과 구분자를 선택한 후 미리보기를 누르세요." : "Choose a file and delimiter, then preview.")
                }
            }
        }
        footer: RowLayout {
            Item { Layout.fillWidth: true }
            Button { text: rootWindow.tr("Cancel"); onClicked: importCsvDialog.reject() }
            Button { text: rootWindow.language === "ko" ? "가져오기" : "Import"; onClicked: importCsvDialog.submitImport() }
        }
        onAccepted: {
            var paths = csvPathField.text.trim().split(/\r?\n/).map(function(path) { return path.trim(); }).filter(function(path) { return path !== ""; });
            mapCanvas.layerSettingsPayload = JSON.stringify({operation: "import-csv", inputPaths: paths, outputName: csvLayerField.text.trim(), xField: csvXField.text.trim(), yField: csvYField.text.trim(), crs: csvCrsField.text.trim().toUpperCase(), encoding: csvEncodingField.text.trim() || "UTF-8", delimiter: csvDelimiterField.text, numberFields: csvNumberFields.text, boolFields: csvBoolFields.text, buildBoundary: csvBuildBoundary.checked, boundaryGroupField: csvBoundaryGroup.text.trim()});
            mapCanvas.layerSettingsGeneration += 1;
        }
    }

    Dialog {
        id: workflowDialog
        objectName: "workflowDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "업무 워크플로우" : "Workflow tools"
        width: Math.min(560, rootWindow.width - 32)
        property var operationCodes: ["measure-layer", "validate-layer", "filter-layer", "polygon-layer", "join-table", "label-field", "lua-filter", "lua-split", "split-boundary"]
        property string selectedOperation: operationCodes[workflowOperation.currentIndex]
        property string errorText: ""
        onOpened: { workflowLayer.text = mapCanvas.activeLayer; errorText = ""; }
        function submitWorkflow() {
            if (workflowLayer.text.trim() === "" ||
                (selectedOperation === "join-table" && (workflowTable.text.trim() === "" || workflowLayerKey.text.trim() === "" || workflowTableKey.text.trim() === "")) ||
                (selectedOperation === "polygon-layer" && workflowFeatureIds.text.trim() === "") ||
                (selectedOperation === "label-field" && workflowLabelField.text.trim() === "") ||
                (selectedOperation === "filter-layer" && workflowFilterField.text.trim() === "") ||
                ((selectedOperation === "lua-filter" || selectedOperation === "lua-split") && workflowLuaScript.text.trim() === "") ||
                (selectedOperation === "split-boundary" && workflowBoundaryLayer.text.trim() === "")) {
                errorText = rootWindow.language === "ko" ? "이 작업에 필요한 입력을 모두 채우세요." : "Fill in the required fields for this tool.";
                return;
            }
            errorText = "";
            accept();
        }
        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 16
            ComboBox { id: workflowOperation; objectName: "workflowOperation"; Layout.fillWidth: true; model: rootWindow.language === "ko" ? ["길이·면적 계산", "데이터 검수", "속성 필터", "점으로 폴리곤", "업무대장 조인", "레이블 필드 생성", "Lua 필터", "Lua로 레이어 나누기", "경계 식별자로 SHP 면 분리"] : ["Calculate length and area", "Validate data", "Filter features", "Polygon from points", "Join table", "Generate label field", "Lua filter", "Split layers with Lua", "Split SHP polygons by boundaries"] }
            TextField { id: workflowLayer; objectName: "workflowLayer"; Layout.fillWidth: true; placeholderText: rootWindow.language === "ko" ? "대상 레이어명" : "Source layer name" }
            TextField { id: workflowOutput; Layout.fillWidth: true; visible: workflowDialog.selectedOperation !== "validate-layer"; placeholderText: rootWindow.language === "ko" ? "결과 레이어명 (선택)" : "Result layer name (optional)" }
            RowLayout {
                Layout.fillWidth: true
                visible: workflowDialog.selectedOperation === "join-table"
                TextField { id: workflowTable; objectName: "workflowTable"; Layout.fillWidth: true; placeholderText: rootWindow.language === "ko" ? "업무대장 CSV/TSV 경로" : "Table CSV/TSV path" }
                Button { text: rootWindow.language === "ko" ? "찾기" : "Browse"; onClicked: workflowTableOpenDialog.open() }
            }
            TextField { id: workflowLayerKey; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "join-table"; placeholderText: rootWindow.language === "ko" ? "레이어 연결 키" : "Layer key" }
            TextField { id: workflowTableKey; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "join-table"; placeholderText: rootWindow.language === "ko" ? "대장 연결 키" : "Table key" }
            TextField { id: workflowEncoding; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "join-table"; placeholderText: "Encoding"; text: "UTF-8" }
            TextField { id: workflowDelimiter; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "join-table"; placeholderText: "Delimiter (, or tab)"; text: "," }
            TextField { id: workflowFeatureIds; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "polygon-layer"; placeholderText: rootWindow.language === "ko" ? "연결 순서대로 피처 ID (예: 1,2,3)" : "Ordered feature IDs, e.g. 1,2,3" }
            RowLayout {
                Layout.fillWidth: true
                visible: workflowDialog.selectedOperation === "validate-layer"
                TextField { id: workflowOutputPath; Layout.fillWidth: true; placeholderText: rootWindow.language === "ko" ? "검수 결과 CSV 경로 (선택)" : "Validation CSV output path (optional)" }
                Button { text: rootWindow.language === "ko" ? "저장 위치" : "Choose"; onClicked: workflowValidationSaveDialog.open() }
            }
            TextField { id: workflowRequired; objectName: "workflowRequired"; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "validate-layer"; placeholderText: rootWindow.language === "ko" ? "필수 필드명 (쉼표로 구분)" : "Required fields (comma-separated)" }
            TextField { id: workflowUnique; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "validate-layer"; placeholderText: rootWindow.language === "ko" ? "고유값 필드명 (선택)" : "Unique key field (optional)" }
            TextField { id: workflowFilterField; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "filter-layer"; placeholderText: rootWindow.language === "ko" ? "필터 필드명" : "Filter field" }
            TextField { id: workflowFilterValue; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "filter-layer"; placeholderText: rootWindow.language === "ko" ? "필터 값" : "Filter value" }
            TextArea { id: workflowLuaScript; objectName: "workflowLuaScript"; Layout.fillWidth: true; Layout.preferredHeight: 80; visible: workflowDialog.selectedOperation === "lua-filter" || workflowDialog.selectedOperation === "lua-split"; placeholderText: workflowDialog.selectedOperation === "lua-split" ? "return feature.grade" : "return feature.grade == \"A\""; wrapMode: TextEdit.Wrap }
            TextField { id: workflowBoundaryLayer; objectName: "workflowBoundaryLayer"; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "split-boundary"; placeholderText: rootWindow.language === "ko" ? "CSV에서 만든 경계 레이어명 (*_boundaries)" : "Boundary layer from CSV (*_boundaries)" }
            TextField { id: workflowLabelField; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "label-field"; placeholderText: rootWindow.language === "ko" ? "새 레이블 필드명" : "New label field name" }
            TextField { id: workflowLabelTemplate; Layout.fillWidth: true; visible: workflowDialog.selectedOperation === "label-field"; placeholderText: rootWindow.language === "ko" ? "레이블 형식 (예: 시설-{관리번호})" : "Label template (e.g. Asset-{id})" }
            Label { Layout.fillWidth: true; visible: workflowDialog.errorText !== ""; color: "#b42318"; text: workflowDialog.errorText; wrapMode: Text.WordWrap }
        }
        footer: RowLayout {
            Item { Layout.fillWidth: true }
            Button { text: rootWindow.tr("Cancel"); onClicked: workflowDialog.reject() }
            Button { text: rootWindow.language === "ko" ? "실행" : "Run"; onClicked: workflowDialog.submitWorkflow() }
        }
        onAccepted: {
            mapCanvas.layerSettingsPayload = JSON.stringify({operation: workflowDialog.selectedOperation, layerName: workflowLayer.text.trim(), outputName: workflowOutput.text.trim(), tablePath: workflowTable.text.trim(), layerKey: workflowLayerKey.text.trim(), tableKey: workflowTableKey.text.trim(), featureIds: workflowFeatureIds.text.trim(), outputPath: workflowOutputPath.text.trim(), filterField: workflowFilterField.text.trim(), filterValue: workflowFilterValue.text, luaScript: workflowLuaScript.text, boundaryLayer: workflowBoundaryLayer.text.trim(), requiredFields: workflowRequired.text, uniqueField: workflowUnique.text.trim(), encoding: workflowEncoding.text.trim(), delimiter: workflowDelimiter.text, field: workflowLabelField.text.trim(), template: workflowLabelTemplate.text});
            mapCanvas.layerSettingsGeneration += 1;
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
        MenuItem {
            objectName: "layerContextToggleGeometryLock"
            text: {
                var index = mapViewport.findLayerIndex(layerContextMenu.targetLayerName);
                var locked = index >= 0 && layerModel.get(index).geometryLocked === true;
                return locked ? "Unlock geometry editing" : "Lock geometry editing";
            }
            enabled: mapViewport.findLayerIndex(layerContextMenu.targetLayerName) >= 0 && layerModel.get(mapViewport.findLayerIndex(layerContextMenu.targetLayerName)).editable === true
            onTriggered: {
                mapCanvas.editAction = "toggleGeometryLock";
                mapCanvas.editValue = layerContextMenu.targetLayerName;
            }
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
        id: spatialOperationDialog
        objectName: "spatialOperationDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "공간 연산" : rootWindow.language === "jp" ? "空間演算" : "Spatial operation"
        standardButtons: Dialog.Ok | Dialog.Cancel
        width: Math.min(520, rootWindow.width - 48)
        property string selectedOperation: spatialOperationChoice.currentValue
        contentItem: ColumnLayout {
            spacing: 10
            Label { text: rootWindow.language === "ko" ? "연산" : rootWindow.language === "jp" ? "演算" : "Operation" }
            ComboBox {
                id: spatialOperationChoice
                objectName: "spatialOperationChoice"
                Layout.fillWidth: true
                model: ["intersect", "union", "difference", "buffer"]
                onCurrentValueChanged: spatialRightLabel.visible = currentValue !== "buffer"
            }
            Label { text: rootWindow.language === "ko" ? "왼쪽 레이어" : rootWindow.language === "jp" ? "左レイヤー" : "Left layer" }
            ComboBox {
                id: spatialLeftChoice
                objectName: "spatialLeftChoice"
                Layout.fillWidth: true
                model: layerModel
                textRole: "displayName"
            }
            Label {
                id: spatialRightLabel
                text: rootWindow.language === "ko" ? "오른쪽 레이어" : rootWindow.language === "jp" ? "右レイヤー" : "Right layer"
                visible: spatialOperationChoice.currentValue !== "buffer"
            }
            ComboBox {
                id: spatialRightChoice
                objectName: "spatialRightChoice"
                Layout.fillWidth: true
                model: layerModel
                textRole: "displayName"
                visible: spatialOperationChoice.currentValue !== "buffer"
            }
            Label {
                text: rootWindow.language === "ko" ? "결과 레이어 이름" : rootWindow.language === "jp" ? "結果レイヤー名" : "Result layer name"
            }
            TextField {
                id: spatialResultName
                objectName: "spatialResultName"
                Layout.fillWidth: true
                placeholderText: "result"
            }
            Label {
                text: rootWindow.language === "ko" ? "버퍼 거리" : rootWindow.language === "jp" ? "バッファ距離" : "Buffer distance"
                visible: spatialOperationChoice.currentValue === "buffer"
            }
            TextField {
                id: spatialDistance
                objectName: "spatialDistance"
                Layout.fillWidth: true
                visible: spatialOperationChoice.currentValue === "buffer"
                text: "10"
                validator: DoubleValidator {}
            }
        }
        onOpened: {
            spatialOperationChoice.currentIndex = 0;
            spatialLeftChoice.currentIndex = 0;
            spatialRightChoice.currentIndex = layerModel.count > 1 ? 1 : 0;
            spatialResultName.text = "result";
            spatialDistance.text = "10";
        }
        onAccepted: {
            if (layerModel.count === 0 || spatialLeftChoice.currentIndex < 0)
                return;
            var left = layerModel.get(spatialLeftChoice.currentIndex).name;
            var right = spatialRightChoice.currentIndex >= 0 && layerModel.count > 0 ? layerModel.get(spatialRightChoice.currentIndex).name : "";
            var result = spatialResultName.text.trim();
            if (result.length === 0) {
                mapCanvas.renderStatus = rootWindow.language === "ko" ? "결과 레이어 이름이 필요합니다" : "Result layer name is required";
                return;
            }
            mapCanvas.spatialPayload = JSON.stringify({operation: spatialOperationChoice.currentValue, left: left, right: right, result: result, distance: Number(spatialDistance.text)});
            mapCanvas.spatialGeneration += 1;
        }
    }

    Dialog {
        id: postGISTableDialog
        objectName: "postGISTableDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "PostGIS 테이블 목록" : rootWindow.language === "jp" ? "PostGISテーブル一覧" : "PostGIS table catalog"
        width: Math.min(620, rootWindow.width - 48)
        height: Math.min(560, rootWindow.height - 80)
        standardButtons: Dialog.Close
        property string errorText: ""
        property bool catalogTruncated: false
        onOpened: {
            errorText = "";
            catalogTruncated = false;
            mapCanvas.postGISCatalogVisible = true;
            // Keep catalog rows lazy: the native payload remains the source
            // of truth while the QML ListView exists only when open.
            mapViewport.postGISCatalogPayloadSeen = -1;
            mapCanvas.postGISCatalogSchema = postGISSchemaField.text.trim();
            mapCanvas.postGISCatalogGeneration += 1;
        }
        onClosed: {
            mapCanvas.postGISCatalogVisible = false;
            mapCanvas.postGISCatalogPayload = "[]";
            mapViewport.postGISTables = [];
            errorText = "";
            catalogTruncated = false;
        }
        contentItem: ColumnLayout {
            spacing: 8
            RowLayout {
                Layout.fillWidth: true
                Label { text: rootWindow.language === "ko" ? "스키마" : rootWindow.language === "jp" ? "スキーマ" : "Schema" }
                TextField {
                    id: postGISSchemaField
                    objectName: "postGISSchemaField"
                    Layout.fillWidth: true
                    placeholderText: "public"
                }
                Button {
                    objectName: "refreshPostGISTablesButton"
                    text: rootWindow.language === "ko" ? "새로고침" : rootWindow.language === "jp" ? "更新" : "Refresh"
                    onClicked: {
                        mapCanvas.postGISCatalogSchema = postGISSchemaField.text.trim();
                        mapCanvas.postGISCatalogGeneration += 1;
                    }
                }
            }
            Label {
                Layout.fillWidth: true
                visible: postGISTableDialog.errorText !== ""
                text: postGISTableDialog.errorText
                color: "#b42318"
                wrapMode: Text.WordWrap
            }
            Label {
                Layout.fillWidth: true
                visible: postGISTableDialog.catalogTruncated
                text: rootWindow.language === "ko" ? "테이블이 많아 처음 2,048개만 표시됩니다. 스키마를 좁혀 다시 조회하세요." : rootWindow.language === "jp" ? "テーブル数が多いため最初の2,048件だけ表示しています。スキーマを絞って再検索してください。" : "There are more tables; only the first 2,048 are shown. Narrow the schema and refresh."
                color: "#8a4b08"
                wrapMode: Text.WordWrap
            }
            Label {
                Layout.fillWidth: true
                text: rootWindow.language === "ko" ? "공간 테이블을 선택하면 레이어로 불러옵니다. 레코드 추가·삭제는 속성 테이블에서 수행합니다." : rootWindow.language === "jp" ? "空間テーブルを選択するとレイヤーとして読み込みます。レコードの追加・削除は属性テーブルで行います。" : "Select a spatial table to load it as a layer. Add or delete records from the attribute table."
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            ListView {
                objectName: "postGISTableList"
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                reuseItems: true
                cacheBuffer: 0
                model: mapViewport.postGISTables
                delegate: ItemDelegate {
                    required property var modelData
                    width: ListView.view.width
                    text: modelData.schema + "." + modelData.name + "  ·  SRID " + modelData.srid
                    onClicked: {
                        mapViewport.requestLoad("postgis://" + modelData.schema + "." + modelData.name);
                        postGISTableDialog.close();
                    }
                }
            }
        }
    }

    Dialog {
        id: deleteRecordDialog
        objectName: "deleteRecordDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        implicitWidth: 420
        title: rootWindow.language === "ko" ? "레코드 삭제 확인" : rootWindow.language === "jp" ? "レコード削除の確認" : "Confirm record deletion"
        standardButtons: Dialog.Ok | Dialog.Cancel
        contentItem: Label {
            text: rootWindow.language === "ko" ? "선택한 레코드를 삭제하고 PostGIS/원본에 저장할까요?" : rootWindow.language === "jp" ? "選択したレコードを削除してPostGIS/ソースに保存しますか？" : "Delete the selected record and save the change to PostGIS/source?"
            wrapMode: Text.WordWrap
        }
        onAccepted: {
            mapCanvas.editAction = "deleteConfirmed";
            mapCanvas.editValue = "";
            mapCanvas.editGeneration += 1;
        }
    }

    Dialog {
        id: addRecordDialog
        objectName: "addRecordDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "레코드 추가" : rootWindow.language === "jp" ? "レコード追加" : "Add record"
        width: Math.min(560, rootWindow.width - 48)
        contentItem: ColumnLayout {
            spacing: 8
            Label { text: "ID" }
            TextField { id: addRecordID; objectName: "addRecordID"; Layout.fillWidth: true; text: "1"; validator: IntValidator {} }
            Label { text: "WKT" }
            TextField { id: addRecordWKT; objectName: "addRecordWKT"; Layout.fillWidth: true; placeholderText: "POINT (127 37)" }
            Label { text: "Properties JSON" }
            TextArea { id: addRecordProperties; objectName: "addRecordProperties"; Layout.fillWidth: true; text: "{}"; wrapMode: TextEdit.NoWrap }
        }
        onOpened: {
            addRecordID.text = "1";
            addRecordWKT.text = "";
            addRecordProperties.text = "{}";
        }
        function submitRecord() {
            var recordIDText = addRecordID.text.trim();
            var recordID = Number(recordIDText);
            if (recordIDText.length === 0 || !isFinite(recordID)) {
                mapCanvas.renderStatus = rootWindow.language === "ko" ? "ID가 필요합니다" : rootWindow.language === "jp" ? "IDが必要です" : "Feature ID is required";
                return;
            }
            if (addRecordWKT.text.trim().length === 0) {
                mapCanvas.renderStatus = rootWindow.language === "ko" ? "WKT가 필요합니다" : rootWindow.language === "jp" ? "WKTが必要です" : "WKT is required";
                return;
            }
            var properties;
            try {
                properties = JSON.parse(addRecordProperties.text);
            } catch (error) {
                mapCanvas.renderStatus = rootWindow.language === "ko" ? "Properties JSON 형식이 잘못되었습니다" : rootWindow.language === "jp" ? "Properties JSONの形式が不正です" : "Properties JSON is invalid";
                return;
            }
            mapCanvas.editAction = "addFeature";
            mapCanvas.editValue = JSON.stringify({layer: mapViewport.activeLayer, id: recordID, wkt: addRecordWKT.text, properties: properties});
            mapCanvas.editGeneration += 1;
            addRecordDialog.accept();
        }
        footer: RowLayout {
            Item { Layout.fillWidth: true }
            Button {
                objectName: "addRecordCancelButton"
                text: rootWindow.tr("Cancel")
                onClicked: addRecordDialog.reject()
            }
            Button {
                objectName: "addRecordAcceptButton"
                text: rootWindow.language === "ko" ? "추가" : rootWindow.language === "jp" ? "追加" : "Add"
                onClicked: addRecordDialog.submitRecord()
            }
        }
    }

    Dialog {
        id: removeLayerDialog
        objectName: "removeLayerDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
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

    Dialog {
        id: projectCRSDialog
        objectName: "projectCRSDialog"
        modal: true
        title: rootWindow.language === "ko" ? "프로젝트 좌표계" : rootWindow.language === "jp" ? "プロジェクト座標系" : "Project coordinate reference system"
        standardButtons: Dialog.Apply | Dialog.Cancel
        width: Math.min(650, rootWindow.width - 48)
        height: Math.min(630, rootWindow.height - 48)
        anchors.centerIn: Overlay.overlay
        property var catalogResponse: {
            if (typeof appCRSCatalogJSON === "undefined")
                return {available: false, error: "", entries: []};
            try {
                return JSON.parse(appCRSCatalogJSON);
            } catch (error) {
                return {available: false, error: String(error), entries: []};
            }
        }
        property var searchResultEntries: []
        property var catalogEntryIndex: ({})
        property var commonCodes: ["EPSG:5186", "EPSG:5179", "EPSG:4326", "EPSG:3857", "EPSG:32652"]
        property var countryOptions: [
            {ko: "대한민국", jp: "韓国", en: "South Korea", aliases: ["korea"], featured: ["EPSG:5186", "EPSG:5179"]},
            {ko: "일본", jp: "日本", en: "Japan", aliases: ["japan"], featured: ["EPSG:6677"]},
            {ko: "중국", jp: "中国", en: "China", aliases: ["china"], featured: ["EPSG:4497"]},
            {ko: "미국", jp: "アメリカ", en: "United States", aliases: ["united states", "usa"], featured: ["EPSG:5070"]},
            {ko: "영국", jp: "イギリス", en: "United Kingdom", aliases: ["united kingdom", "great britain"], featured: ["EPSG:27700"]},
            {ko: "독일", jp: "ドイツ", en: "Germany", aliases: ["germany"], featured: ["EPSG:25832"]},
            {ko: "프랑스", jp: "フランス", en: "France", aliases: ["france"], featured: ["EPSG:2154"]},
            {ko: "호주", jp: "オーストラリア", en: "Australia", aliases: ["australia"], featured: ["EPSG:7855"]},
            {ko: "캐나다", jp: "カナダ", en: "Canada", aliases: ["canada"], featured: ["EPSG:3347"]},
            {ko: "브라질", jp: "ブラジル", en: "Brazil", aliases: ["brazil"], featured: ["EPSG:5880"]},
            {ko: "인도", jp: "インド", en: "India", aliases: ["india"], featured: ["EPSG:32643"]},
            {ko: "뉴질랜드", jp: "ニュージーランド", en: "New Zealand", aliases: ["new zealand"], featured: ["EPSG:2193"]}
        ]
        function catalogEntry(code) {
            return catalogEntryIndex[code] || null;
        }
        function rebuildCatalogEntryIndex() {
            var next = {};
            var entries = catalogResponse.entries || [];
            for (var i = 0; i < entries.length; ++i) {
                var entry = entries[i];
                if (entry && entry.code && next[entry.code] === undefined)
                    next[entry.code] = entry;
            }
            catalogEntryIndex = next;
        }
        function entriesForCodes(codes) {
            var result = [];
            for (var i = 0; i < codes.length; ++i) {
                var entry = catalogEntry(codes[i]);
                if (entry)
                    result.push(entry);
                else if (!catalogResponse.available)
                    result.push({code: codes[i], name: codes[i]});
            }
            return result;
        }
        function areaMatchesCountry(area, country) {
            for (var i = 0; i < country.aliases.length; ++i) {
                var words = new RegExp("\\b" + country.aliases[i] + "\\b", "i");
                if (words.test(area))
                    return true;
            }
            return false;
        }
        function countryIndexForCurrentCRS() {
            var current = catalogEntry(mapViewport.dataCRS);
            var area = current ? (current.area || "") : "";
            for (var i = 0; i < countryOptions.length; ++i) {
                if (areaMatchesCountry(area, countryOptions[i]))
                    return i;
            }
            return 0;
        }
        function countryEntries(index) {
            var country = countryOptions[index];
            if (!country)
                return [];
            var result = entriesForCodes(country.featured);
            var seen = {};
            for (var i = 0; i < result.length; ++i)
                seen[result[i].code] = true;
            if (!catalogResponse.available)
                return result;
            var entries = catalogResponse.entries || [];
            for (var entryIndex = 0; entryIndex < entries.length && result.length < 80; ++entryIndex) {
                var entry = entries[entryIndex];
                if (entry.kind !== "projected" || seen[entry.code])
                    continue;
                if (areaMatchesCountry(entry.area || "", country)) {
                    result.push(entry);
                    seen[entry.code] = true;
                }
            }
            return result;
        }
        function refreshSearchEntries() {
            if (!catalogResponse.available)
                return searchResultEntries = [];
            var needle = crsSearchField.text.trim().toLowerCase();
            var entries = catalogResponse.entries || [];
            if (needle === "") {
                searchResultEntries = entries;
                return;
            }
            var result = [];
            for (var i = 0; i < entries.length; ++i) {
                var entry = entries[i];
                if (entry.code.toLowerCase().indexOf(needle) >= 0 ||
                    entry.name.toLowerCase().indexOf(needle) >= 0 ||
                    (entry.area || "").toLowerCase().indexOf(needle) >= 0)
                    result.push(entry);
            }
            searchResultEntries = result;
        }
        function scheduleSearchRefresh() {
            crsSearchTimer.restart();
        }
        Timer {
            id: crsSearchTimer
            objectName: "crsSearchTimer"
            interval: 120
            repeat: false
            onTriggered: projectCRSDialog.refreshSearchEntries()
        }
        onCatalogResponseChanged: {
            rebuildCatalogEntryIndex();
            scheduleSearchRefresh();
        }
        onOpened: {
            rebuildCatalogEntryIndex();
            projectCRSField.text = mapViewport.dataCRS || "";
            crsCategoryTabs.currentIndex = 0;
            countryChoice.currentIndex = countryIndexForCurrentCRS();
            crsSearchField.text = "";
            scheduleSearchRefresh();
        }
        onClosed: {
            crsSearchTimer.stop();
            searchResultEntries = [];
        }
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.language === "ko" ? "좌표계를 선택하거나 EPSG 코드를 입력하세요. 적용하면 모든 프로젝트 레이어를 재투영하며 원본 파일은 변경하지 않습니다." : rootWindow.language === "jp" ? "座標系を選択するかEPSGコードを入力してください。適用すると全レイヤーを再投影します。元ファイルは変更しません。" : "Select a CRS or enter an EPSG code. Applying reprojects all project layers without changing source files."
                wrapMode: Text.WordWrap
            }
            RowLayout {
                Layout.fillWidth: true
                Label {
                    text: rootWindow.language === "ko" ? "선택한 좌표계" : rootWindow.language === "jp" ? "選択した座標系" : "Selected CRS"
                }
                TextField {
                    id: projectCRSField
                    objectName: "projectCRSField"
                    Layout.fillWidth: true
                    placeholderText: "EPSG:5186"
                }
            }
            Label {
                objectName: "selectedCRSDetails"
                Layout.fillWidth: true
                property var entry: projectCRSDialog.catalogEntry(projectCRSField.text.trim().toUpperCase())
                text: entry ? entry.name + (entry.area ? " · " + entry.area : "") :
                      (rootWindow.language === "ko" ? "목록 외 코드: 적용할 때 유효성을 확인합니다." :
                       rootWindow.language === "jp" ? "一覧外のコードは適用時に検証します。" :
                       "Codes outside the catalog are validated when applied.")
                color: "#526779"
                elide: Text.ElideRight
                ToolTip.visible: selectedCRSMouse.containsMouse
                ToolTip.text: text
                MouseArea { id: selectedCRSMouse; anchors.fill: parent; hoverEnabled: true }
            }
            TabBar {
                id: crsCategoryTabs
                objectName: "crsCategoryTabs"
                Layout.fillWidth: true
                TabButton { text: rootWindow.language === "ko" ? "자주 사용" : rootWindow.language === "jp" ? "よく使う" : "Common" }
                TabButton { text: rootWindow.language === "ko" ? "국가별 추천" : rootWindow.language === "jp" ? "国別の候補" : "By country" }
                TabButton { text: rootWindow.language === "ko" ? "전체 EPSG 검색" : rootWindow.language === "jp" ? "全EPSGを検索" : "Search all EPSG" }
            }
            StackLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                currentIndex: crsCategoryTabs.currentIndex
                ListView {
                    objectName: "commonCRSList"
                    clip: true
                    reuseItems: true
                    cacheBuffer: 0
                    model: projectCRSDialog.entriesForCodes(projectCRSDialog.commonCodes)
                    delegate: ItemDelegate {
                        required property var modelData
                        required property int index
                        width: ListView.view.width
                        text: modelData.code + "  " + modelData.name
                        highlighted: projectCRSField.text === modelData.code
                        ToolTip.visible: hovered && !!modelData.area
                        ToolTip.text: modelData.area || ""
                        onClicked: projectCRSField.text = modelData.code
                    }
                    ScrollBar.vertical: ScrollBar {}
                }
                ColumnLayout {
                    ComboBox {
                        id: countryChoice
                        objectName: "countryCRSChoice"
                        Layout.fillWidth: true
                        model: projectCRSDialog.countryOptions
                        textRole: rootWindow.language === "ko" ? "ko" : rootWindow.language === "jp" ? "jp" : "en"
                    }
                    Label {
                        Layout.fillWidth: true
                        text: rootWindow.language === "ko" ? "설치된 PROJ의 사용 지역 기준 추천입니다. 다른 좌표계는 전체 EPSG 검색에서 찾으세요." : rootWindow.language === "jp" ? "インストール済みPROJの使用地域に基づく候補です。ほかの座標系は全EPSG検索を使用してください。" : "Suggestions use the installed PROJ area of use. Search all EPSG for other systems."
                        color: "#65717d"
                        wrapMode: Text.WordWrap
                    }
                    ListView {
                        objectName: "nearbyCRSList"
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        reuseItems: true
                        cacheBuffer: 0
                        model: projectCRSDialog.countryEntries(countryChoice.currentIndex)
                        delegate: ItemDelegate {
                            required property var modelData
                            required property int index
                            width: ListView.view.width
                            text: modelData.code + "  " + modelData.name
                            highlighted: projectCRSField.text === modelData.code
                            ToolTip.visible: hovered && !!modelData.area
                            ToolTip.text: modelData.area || ""
                            onClicked: projectCRSField.text = modelData.code
                        }
                        ScrollBar.vertical: ScrollBar {}
                    }
                }
                ColumnLayout {
                    TextField {
                        id: crsSearchField
                        objectName: "crsSearchField"
                        Layout.fillWidth: true
                        placeholderText: rootWindow.language === "ko" ? "EPSG 코드, 이름 또는 사용 지역 검색" : rootWindow.language === "jp" ? "EPSGコード、名前、地域を検索" : "Search code, name, or area"
                    }
                    Label {
                        Layout.fillWidth: true
                        visible: !projectCRSDialog.catalogResponse.available
                        text: rootWindow.language === "ko" ? "전체 목록은 PROJ가 포함된 desktop-native 빌드에서 사용할 수 있습니다." : rootWindow.language === "jp" ? "全リストにはPROJ対応のdesktop-nativeビルドが必要です。" : "The full catalog requires the PROJ-enabled desktop-native build."
                        wrapMode: Text.WordWrap
                    }
                    ListView {
                        id: allCRSList
                        objectName: "allCRSList"
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        reuseItems: true
                        cacheBuffer: 0
                        model: projectCRSDialog.searchResultEntries
                        delegate: ItemDelegate {
                            required property var modelData
                            required property int index
                            width: ListView.view.width
                            text: modelData.code + "  " + modelData.name
                            highlighted: projectCRSField.text === modelData.code
                            ToolTip.visible: hovered && !!modelData.area
                            ToolTip.text: modelData.area || ""
                            onClicked: projectCRSField.text = modelData.code
                        }
                        ScrollBar.vertical: ScrollBar {}
                    }
                    Connections {
                        target: crsSearchField
                        function onTextChanged() { projectCRSDialog.scheduleSearchRefresh(); }
                    }
                    Label {
                        text: rootWindow.language === "ko" ? allCRSList.count + "개 좌표계" : rootWindow.language === "jp" ? allCRSList.count + "件" : allCRSList.count + " CRSs"
                    }
                }
            }
        }
        onAccepted: {
            mapCanvas.layerSettingsPayload = JSON.stringify({operation: "project-crs", projectCrs: projectCRSField.text.trim().toUpperCase()});
            mapCanvas.layerSettingsGeneration += 1;
        }
    }

    Dialog {
        id: layerColorPicker
        objectName: "layerColorPicker"
        title: rootWindow.language === "ko" ? "레이어 색상 선택" : rootWindow.language === "jp" ? "レイヤーの色を選択" : "Choose layer color"
        modal: true
        width: Math.min(470, rootWindow.width - 48)
        anchors.centerIn: Overlay.overlay
        property var targetField: null
        property string selectedColor: "#2b6cb0"
        property var presetColors: ["#2b6cb0", "#356b53", "#d1495b", "#e09f3e",
                                    "#7b2cbf", "#00a6a6", "#5a6772", "#202124",
                                    "#ffffff", "#f4d35e"]
        function normalizedSelectedColor() {
            var value = selectedColor.toLowerCase();
            if (/^#[0-9a-f]{8}$/.test(value))
                value = "#" + value.slice(-6);
            return /^#[0-9a-f]{6}$/.test(value) ? value : "";
        }
        function rgbHex(color) {
            function channel(value) {
                return ("0" + Math.round(Math.max(0, Math.min(1, value)) * 255).toString(16)).slice(-2);
            }
            return "#" + channel(color.r) + channel(color.g) + channel(color.b);
        }
        onSelectedColorChanged: presetHexField.text = selectedColor
        onOpened: presetHexField.text = selectedColor
        contentItem: ColumnLayout {
            spacing: 12
            Label {
                text: rootWindow.language === "ko" ? "빠른 선택" : rootWindow.language === "jp" ? "プリセット" : "Preset colors"
                font.bold: true
            }
            Flow {
                Layout.fillWidth: true
                Layout.preferredHeight: 32
                spacing: 8
                Repeater {
                    id: presetColorRepeater
                    objectName: "presetColorRepeater"
                    model: layerColorPicker.presetColors
                    delegate: Rectangle {
                        required property string modelData
                        width: 32
                        height: 32
                        radius: 4
                        color: modelData
                        border.color: layerColorPicker.selectedColor.toLowerCase() === modelData ? "#173c53" : "#8694a0"
                        border.width: layerColorPicker.selectedColor.toLowerCase() === modelData ? 3 : 1
                        ToolTip.visible: presetMouse.containsMouse
                        ToolTip.text: modelData
                        MouseArea {
                            id: presetMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            onClicked: {
                                layerColorPicker.selectedColor = modelData;
                                // Also repair an invalid manual value when the
                                // user reselects the already active preset.
                                presetHexField.text = modelData;
                            }
                        }
                    }
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Rectangle {
                    Layout.preferredWidth: 48
                    Layout.preferredHeight: 32
                    radius: 4
                    color: layerColorPicker.selectedColor
                    border.color: "#8694a0"
                }
                TextField {
                    id: presetHexField
                    objectName: "presetHexField"
                    Layout.fillWidth: true
                    placeholderText: "#RRGGBB"
                    validator: RegularExpressionValidator { regularExpression: /^#[0-9a-fA-F]{6}$/ }
                    onTextEdited: {
                        if (acceptableInput)
                            layerColorPicker.selectedColor = text.toLowerCase();
                    }
                }
                Button {
                    objectName: "customColorPickerButton"
                    text: rootWindow.language === "ko" ? "상세 색상…" : rootWindow.language === "jp" ? "その他の色…" : "More colors…"
                    onClicked: {
                        nativeLayerColorPicker.selectedColor = layerColorPicker.selectedColor;
                        nativeLayerColorPicker.open();
                    }
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Item { Layout.fillWidth: true }
                Button {
                    text: rootWindow.tr("Cancel")
                    onClicked: layerColorPicker.reject()
                }
                Button {
                    objectName: "applyPickedColorButton"
                    text: rootWindow.language === "ko" ? "색상 선택" : rootWindow.language === "jp" ? "色を選択" : "Use color"
                    enabled: presetHexField.acceptableInput
                    onClicked: layerColorPicker.accept()
                }
            }
        }
        onAccepted: {
            if (targetField && normalizedSelectedColor() !== "")
                targetField.text = normalizedSelectedColor();
            targetField = null;
        }
        onRejected: targetField = null
    }

    QuickDialogs.ColorDialog {
        id: nativeLayerColorPicker
        objectName: "nativeLayerColorPicker"
        title: layerColorPicker.title
        onAccepted: {
            var color = layerColorPicker.rgbHex(selectedColor);
            layerColorPicker.selectedColor = color;
            presetHexField.text = color;
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
                    reuseItems: true
                    cacheBuffer: 0
                    model: ListModel {
                        id: layerModel
                        objectName: "layerModel"
                    }
                    delegate: CheckDelegate {
                        required property string name
                        required property string displayName
                        required property string sourceError
                        required property bool layerVisible
                        required property int index
                        objectName: "layerDelegate_" + name
                        width: ListView.view.width
                        implicitHeight: 36
                        text: (sourceError ? "⚠ " : "") + (displayName === "" ? name : displayName)
                        checked: layerVisible
                        onClicked: {
                            if (index < 0 || index >= layerModel.count || layerModel.get(index).name !== name)
                                return;
                            if (layerModel.get(index).layerVisible !== checked) {
                                layerModel.setProperty(index, "layerVisible", checked);
                                mapViewport.syncLayerVisibility();
                            }
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
                property real previousViewportWidth: 0
                property real previousViewportHeight: 0
                property string selectedLayer: ""
                property string selectedFeature: ""
                property string selectedStatus: "No feature selected"
                property string editorValue: ""
                property int attributePayloadGenerationSeen: -1
                property int layerTreePayloadGenerationSeen: -1
                property string lastSyncedLayerVisibilityPayload: ""
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
                readonly property int maxAttributeRows: 128
                readonly property int maxAttributeColumns: 256
                property int layerLabelGenerationSeen: -1
                property int vertexHandleGenerationSeen: -1
                property string renderStatus: "Ready"
                property var postGISTables: []
                property string postGISCatalogPayloadSeen: ""

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

                QtObject {
                    id: attributeModel
                    objectName: "attributeModel"
                    property var rows: []
                    readonly property int count: rows.length
                }
                QtObject {
                    id: mapLabelModel
                    objectName: "mapLabelModel"
                    property var rows: []
                    readonly property int count: rows.length
                    function get(index) {
                        return rows[index];
                    }
                }
                QtObject {
                    id: vertexHandleModel
                    objectName: "vertexHandleModel"
                    property var rows: []
                    readonly property int count: rows.length
                }

                // Release transient delegate data while the window is not
                // usable. The active poll repopulates it after restoration.
                function releaseTransientWindowModels() {
                    attributeModel.rows = [];
                    mapLabelModel.rows = [];
                    vertexHandleModel.rows = [];
                }

                function restoreTransientWindowModels() {
                    attributePayloadGenerationSeen = -1;
                    layerLabelGenerationSeen = -1;
                    vertexHandleGenerationSeen = -1;
                }

                function parseJSONOr(value, fallback) {
                    try {
                        return JSON.parse(value);
                    } catch (error) {
                        return fallback;
                    }
                }

                function parseJSONArray(value) {
                    var parsed = parseJSONOr(value, []);
                    return Array.isArray(parsed) ? parsed : [];
                }

                function parseJSONObject(value) {
                    var parsed = parseJSONOr(value, ({}));
                    return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : ({});
                }

                function layerTreeRow(layer) {
                    return {
                        name: layer.name,
                        displayName: layer.displayName || layer.name,
                        sourcePath: layer.sourcePath || "",
                        sourceLayerName: layer.sourceLayerName || "",
                        sourceEncoding: layer.sourceEncoding || "",
                        sourceCrs: layer.sourceCrs || "",
                        sourceError: layer.sourceError || "",
                        crs: layer.crs || "",
                        geometryType: layer.geometryType || "",
                        boundsJson: JSON.stringify(layer.bounds || []),
                        layerVisible: layer.visible !== false,
                        editable: layer.editable === true,
                        geometryLocked: layer.geometryLocked === true,
                        displayRule: layer.displayRule || "",
                        style: layer.style || ({}),
                        labels: layer.labels || ({
                            enabled: false,
                            expression: "",
                            placement: "center",
                            heightMm: 2.5
                        })
                    };
                }

                function sameLayerObjectFields(left, right, fields) {
                    left = left || ({});
                    right = right || ({});
                    for (var fieldIndex = 0; fieldIndex < fields.length; ++fieldIndex) {
                        var field = fields[fieldIndex];
                        if (left[field] !== right[field])
                            return false;
                    }
                    return true;
                }

                function sameLayerStyle(left, right) {
                    return sameLayerObjectFields(left, right, [
                        "pointColor", "lineColor", "polygonColor", "pointSizeMm",
                        "lineWidthMm", "fillOpacity"
                    ]);
                }

                function sameLayerLabels(left, right) {
                    return sameLayerObjectFields(left, right, [
                        "enabled", "expression", "luaScript", "placement", "rotationField",
                        "pointPlacement", "pointOffsetMm", "heightMm", "minScale", "maxScale", "rule"
                    ]);
                }

                // Preserve existing ListView delegates when a layer's metadata
                // changes. Clearing and appending the whole model causes every
                // layer row to be destroyed/recreated, which is expensive and
                // visibly resets hover/scroll state on large projects.
                function replaceLayerModel(layers) {
                    var roleNames = ["name", "displayName", "sourcePath", "sourceLayerName",
                                     "sourceEncoding", "sourceCrs", "sourceError", "crs",
                                     "geometryType", "boundsJson", "layerVisible", "editable", "geometryLocked", "displayRule",
                                     "style", "labels"];
                    var sharedCount = Math.min(layerModel.count, layers.length);
                    for (var index = 0; index < sharedCount; ++index) {
                        var next = layerTreeRow(layers[index]);
                        var current = layerModel.get(index);
                        for (var roleIndex = 0; roleIndex < roleNames.length; ++roleIndex) {
                            var role = roleNames[roleIndex];
                            var changed;
                            if (role === "style")
                                changed = !sameLayerStyle(current[role], next[role]);
                            else if (role === "labels")
                                changed = !sameLayerLabels(current[role], next[role]);
                            else
                                changed = current[role] !== next[role];
                            if (changed)
                                layerModel.setProperty(index, role, next[role]);
                        }
                    }
                    while (layerModel.count > layers.length)
                        layerModel.remove(layerModel.count - 1);
                    for (var appendIndex = sharedCount; appendIndex < layers.length; ++appendIndex)
                        layerModel.append(layerTreeRow(layers[appendIndex]));
                }

                function anyLayerVisible() {
                    for (var i = 0; i < layerModel.count; ++i) {
                        if (layerModel.get(i).layerVisible)
                            return true;
                    }
                    return false;
                }

                function activeLayerEditable() {
                    for (var i = 0; i < layerModel.count; ++i) {
                        var layer = layerModel.get(i);
                        if (layer.name === activeLayer)
                            return layer.editable === true;
                    }
                    return false;
                }

                function activeLayerGeometryEditable() {
                    for (var i = 0; i < layerModel.count; ++i) {
                        var layer = layerModel.get(i);
                        if (layer.name === activeLayer)
                            return layer.editable === true && layer.geometryLocked !== true;
                    }
                    return false;
                }

                function selectLayer(name) {
                    var layerChanged = activeLayer !== name || mapCanvas.activeLayer !== name;
                    if (layerChanged)
                        attributeFieldHints = [];
                    activeLayer = name;
                    if (mapCanvas.activeLayer !== name) {
                        mapCanvas.activeLayer = name;
                        mapCanvas.activeLayerGeneration += 1;
                    }
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

                function zoomByFactor(factor) {
                    var oldZoom = mapZoom;
                    if (!isFinite(oldZoom) || oldZoom <= 0)
                        return false;
                    var newZoom = Math.max(0.25, Math.min(maxMapZoom(), oldZoom * factor));
                    if (Math.abs(newZoom - oldZoom) < 0.000001)
                        return false;
                    var ratio = newZoom / oldZoom;
                    mapZoom = newZoom;
                    // Keep the current map center fixed while zooming from a button.
                    panX *= ratio;
                    panY *= ratio;
                    cursorValid = false;
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

                function preserveViewOnResize() {
                    var oldWidth = previousViewportWidth;
                    var oldHeight = previousViewportHeight;
                    // Full-screen transitions can briefly expose a zero-sized
                    // viewport. Keep the last valid dimensions so the final
                    // resize still preserves the map's world center.
                    if (width <= 0 || height <= 0)
                        return;
                    previousViewportWidth = width;
                    previousViewportHeight = height;
                    if (oldWidth <= 0 || oldHeight <= 0 ||
                            !hasMapMetadata || layerModel.count === 0 || mapZoom <= 0)
                        return;
                    var aspect = mapMeterAspectRatio(dataBounds, dataCRS);
                    var oldCanvasWidth = Math.min(oldWidth, oldHeight * aspect);
                    var oldCanvasHeight = Math.min(oldHeight, oldWidth / aspect);
                    var newCanvasWidth = Math.min(width, height * aspect);
                    var newCanvasHeight = Math.min(height, width / aspect);
                    if (oldCanvasWidth <= 0 || oldCanvasHeight <= 0 || newCanvasWidth <= 0 || newCanvasHeight <= 0)
                        return;
                    // Zoom is relative to the extent-sized canvas, not the viewport.
                    // Resizing without adjusting it shifts a deeply panned map away
                    // from the screen and changes the displayed metres per pixel.
                    var centerX = 0.5 - panX / (oldCanvasWidth * mapZoom);
                    var centerY = 0.5 + panY / (oldCanvasHeight * mapZoom);
                    var resizedZoom = mapZoom * oldCanvasWidth / newCanvasWidth;
                    mapZoom = resizedZoom;
                    panX = (0.5 - centerX) * newCanvasWidth * resizedZoom;
                    panY = (centerY - 0.5) * newCanvasHeight * resizedZoom;
                    cursorValid = false;
                    viewportGeneration += 1;
                }

                onWidthChanged: {
                    preserveViewOnResize();
                    if (pendingInitialLayerFit)
                        Qt.callLater(function() { mapViewport.fitInitialMapExtent(); });
                }
                onHeightChanged: {
                    preserveViewOnResize();
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
                    return formatMapCoordinate(x, y);
                }

                function formatMapCoordinate(x, y) {
                    function formatValue(value) {
                        var fixed = Number(value).toFixed(4);
                        var parts = fixed.split(".");
                        var integer = parts[0];
                        var sign = "";
                        if (integer.charAt(0) === "-" || integer.charAt(0) === "+") {
                            sign = integer.charAt(0);
                            integer = integer.slice(1);
                        }
                        integer = integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
                        return sign + integer + "." + parts[1];
                    }
                    return "X " + formatValue(x) + "  Y " + formatValue(y);
                }

                function centerMapCoordinateValues() {
                    if (layerModel.count === 0 || mapCanvas.width <= 0 || mapCanvas.height <= 0 || mapZoom <= 0)
                        return null;
                    var bounds = dataBounds;
                    if (bounds.length < 4 || bounds[2] <= bounds[0] || bounds[3] <= bounds[1])
                        return null;
                    var nx = 0.5 - panX / (mapCanvas.width * mapZoom);
                    var ny = 0.5 + panY / (mapCanvas.height * mapZoom);
                    return [bounds[0] + nx * (bounds[2] - bounds[0]),
                            bounds[1] + ny * (bounds[3] - bounds[1])];
                }

                function statusCoordinateText() {
                    if (cursorValid)
                        return (rootWindow.language === "ko" ? "커서  " : rootWindow.language === "jp" ? "カーソル  " : "Cursor  ") + currentMapCoordinate();
                    var center = centerMapCoordinateValues();
                    return (rootWindow.language === "ko" ? "중심  " : rootWindow.language === "jp" ? "中心  " : "Center  ") +
                           (center ? formatMapCoordinate(center[0], center[1]) : "—");
                }

                function luaFieldAccess(name) {
                    var keywords = ["and", "break", "do", "else", "elseif", "end", "false", "for",
                                    "function", "if", "in", "local", "nil", "not", "or", "repeat",
                                    "return", "then", "true", "until", "while"];
                    if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) && keywords.indexOf(name) < 0)
                        return "feature." + name;
                    var quoted = '"';
                    for (var index = 0; index < name.length; ++index) {
                        var code = name.charCodeAt(index);
                        if (code === 34) quoted += '\\"';
                        else if (code === 92) quoted += "\\\\";
                        else if (code < 32 || code === 127)
                            quoted += "\\" + ("000" + String(code)).slice(-3);
                        else quoted += name[index];
                    }
                    return "feature[" + quoted + '"]';
                }

                function luaFieldHintText() {
                    if (!attributeFieldHints.length)
                        return rootWindow.language === "ko" ? "필드를 불러오는 중이거나 사용할 수 없습니다" : rootWindow.language === "jp" ? "フィールドを読み込み中、または利用できません" : "Fields are loading or unavailable";
                    var count = attributeFieldHints.length;
                    return rootWindow.language === "ko" ? count + "개 필드 사용 가능" : rootWindow.language === "jp" ? count + " 件のフィールドを利用可能" : count + (count === 1 ? " field available" : " fields available");
                }

                function currentScaleText() {
                    if (layerModel.count === 0 || mapCanvas.width <= 0 || mapZoom <= 0)
                        return "—";
                    var bounds = dataBounds;
                    var xUnitsPerPixel = (bounds[2] - bounds[0]) / (mapCanvas.width * mapZoom);
                    var metersPerUnit = 1.0;
                    if (dataCRS.toUpperCase() === "EPSG:4326") {
                        var centerLatitude = (bounds[1] + bounds[3]) / 2;
                        metersPerUnit = 111319.49 * Math.max(0.01, Math.cos(centerLatitude * Math.PI / 180));
                    }
                    var denominator = xUnitsPerPixel * metersPerUnit * 96 / 0.0254;
                    if (!isFinite(denominator) || denominator <= 0)
                        return "—";
                    return "≈ 1:" + Math.max(1, Math.round(denominator)).toLocaleString();
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
                    var xText = coordinateXInput.text.trim();
                    var yText = coordinateYInput.text.trim();
                    var x = Number(xText);
                    var y = Number(yText);
                    if (!xText || !yText || !isFinite(x) || !isFinite(y) || dataBounds.length < 4 || dataBounds[2] === dataBounds[0] || dataBounds[3] === dataBounds[1]) {
                        coordinateNavigationStatus.text = rootWindow.language === "ko" ? "유효한 지도 좌표를 입력하세요." : rootWindow.language === "jp" ? "有効な地図座標を入力してください。" : "Enter valid map coordinates.";
                        return false;
                    }
                    var nx = (x - dataBounds[0]) / (dataBounds[2] - dataBounds[0]);
                    var ny = (y - dataBounds[1]) / (dataBounds[3] - dataBounds[1]);
                    panX = (0.5 - nx) * mapCanvas.width * mapZoom;
                    panY = (ny - 0.5) * mapCanvas.height * mapZoom;
                    viewportGeneration += 1;
                    coordinateNavigationStatus.text = "";
                    return true;
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
                    var payload = JSON.stringify(visibility);
                    if (payload === lastSyncedLayerVisibilityPayload)
                        return;
                    lastSyncedLayerVisibilityPayload = payload;
                    mapCanvas.layerVisibilityPayload = payload;
                    mapCanvas.layerVisibilityGeneration += 1;
                }

                MapCanvas {
                    id: mapCanvas
                    objectName: "goGisMapCanvas"
                    // The native bridge uses this state to release scene data
                    // while the window is minimized, then repaints on restore.
                    property bool windowActive: rootWindow.visible && rootWindow.visibility !== Window.Minimized
                    property real mapAspectRatio: {
                        return mapViewport.mapMeterAspectRatio(mapViewport.dataBounds, mapViewport.dataCRS);
                    }
                    property real viewportPanX: mapViewport.panX
                    property real viewportPanY: mapViewport.panY
                    // Native render planning must observe the final pan and
                    // zoom after a resize, even when the canvas item itself
                    // does not change size along one axis.
                    property int viewGeneration: mapViewport.viewportGeneration
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
                    property bool attributePayloadVisible: false
                    property int attributePayloadGeneration: 0
                    onAttributePayloadChanged: attributePayloadGeneration += 1
                    property int attributePage: 0
                    property int attributePageGeneration: 0
                    property string layerTreePayload: "[]"
                    property int layerTreePayloadGeneration: 0
                    onLayerTreePayloadChanged: layerTreePayloadGeneration += 1
                    property string layerSettingsPayload: ""
                    property int layerSettingsGeneration: 0
                    property string spatialPayload: ""
                    property int spatialGeneration: 0
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
                    property string postGISCatalogSchema: ""
                    property int postGISCatalogGeneration: 0
                    property string postGISCatalogPayload: "[]"
                    property int postGISCatalogPayloadGeneration: 0
                    onLoadCapturedGenerationChanged: {
                        var requests = mapViewport.parseJSONArray(loadRequestJournal);
                        loadRequestJournal = JSON.stringify(requests.filter(function(request) {
                            return request.generation > loadCapturedGeneration;
                        }));
                    }
                    property string savePath: ""
                    property string saveProfile: ""
                    property string saveOptions: ""
                    property int saveGeneration: 0
                    scale: mapViewport.mapZoom
                    transformOrigin: Item.Center
                    visible: mapViewport.anyLayerVisible()
                    onVisibleChanged: {
                        if (!visible) {
                            mapLabelModel.rows = [];
                            vertexHandleModel.rows = [];
                        } else {
                            mapViewport.layerLabelGenerationSeen = -1;
                            mapViewport.vertexHandleGenerationSeen = -1;
                        }
                    }
                }

                Repeater {
                    objectName: "mapLabelRepeater"
                    model: mapLabelModel.rows
                    delegate: Text {
                        required property var modelData
                        property var label: modelData
                        z: 2
                        x: mapCanvas.x + mapCanvas.width / 2 + (label.x - 0.5) * mapCanvas.width * mapCanvas.scale - width / 2 + Number(label.offsetXmm || 0) * mapCanvas.logicalPixelsPerMm
                        y: mapCanvas.y + mapCanvas.height / 2 - (label.y - 0.5) * mapCanvas.height * mapCanvas.scale - height / 2 + Number(label.offsetYmm || 0) * mapCanvas.logicalPixelsPerMm
                        rotation: mapViewport.screenRotation(label.rotation)
                        text: label.text
                        textFormat: Text.PlainText
                        font.pixelSize: Math.max(1, Math.round(label.heightMm * mapCanvas.logicalPixelsPerMm))
                        color: "#17212b"
                        style: Text.Outline
                        styleColor: "#ffffff"
                        // Go filters labels by layer visibility, viewport, scale,
                        // decluttering, and density before publishing this model.
                        // Repeating a full layer scan for every label here made
                        // visibility updates O(labels * layers) on the GUI thread.
                        visible: true
                    }
                }

                Repeater {
                    objectName: "vertexHandleRepeater"
                    z: 15
                    // Keep the bounded handle data available for the edit
                    // action, but do not allocate interactive QML delegates
                    // while vertex editing is not active.
                    model: rootWindow.vertexEditMode && mapCanvas.visible ? vertexHandleModel.rows : []
                    delegate: Rectangle {
                        required property var modelData
                        id: vertexHandle
                        property var handle: modelData || ({})
                        property alias dragArea: vertexMouseArea
                        objectName: "vertexHandle_" + handle.vertexIndex
                        z: 20
                        width: 12
                        height: 12
                        radius: 6
                        x: mapCanvas.x + mapCanvas.width / 2 + (handle.x - 0.5) * mapCanvas.width * mapCanvas.scale - width / 2
                        y: mapCanvas.y + mapCanvas.height / 2 - (handle.y - 0.5) * mapCanvas.height * mapCanvas.scale - height / 2
                        color: "#fff"
                        border.color: "#145da0"
                        border.width: 2
                        visible: rootWindow.vertexEditMode
                        MouseArea {
                            id: vertexMouseArea
                            objectName: "vertexDragArea_" + vertexHandle.handle.vertexIndex
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
                                mapCanvas.editValue = JSON.stringify({vertexIndex: vertexHandle.handle.vertexIndex, x: bounds[0] + nx * (bounds[2] - bounds[0]), y: bounds[1] + ny * (bounds[3] - bounds[1])});
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
                    var pending = mapViewport.parseJSONArray(mapCanvas.loadRequestJournal);
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
                    objectName: "mapStateSyncTimer"
                    // Keep rendering and vertex-edit updates at 30 Hz, but
                    // avoid waking the QML/JSON bridge at that rate when the
                    // map is completely idle. Dialogs use generation-gated
                    // payloads and remain responsive at the idle cadence.
                    interval: rootWindow.statusIsBusy(mapCanvas.renderStatus) || rootWindow.vertexEditMode ? 33 : 125
                    repeat: true
                    running: rootWindow.visible && rootWindow.visibility !== Window.Minimized
                    onTriggered: {
                        if (mapViewport.selectedFeature !== mapCanvas.selectionFeature) {
                            mapViewport.editorValue = mapCanvas.selectionValue;
                        }
                        if (mapCanvas.attributePayloadGeneration !== mapViewport.attributePayloadGenerationSeen) {
                            mapViewport.attributePayloadGenerationSeen = mapCanvas.attributePayloadGeneration;
                            var fieldHintsVisible = attributeDialog.visible || layerSettingsDialog.visible;
                            if (fieldHintsVisible) {
                                var table = mapViewport.parseJSONObject(mapCanvas.attributePayload);
                                var columns = Array.isArray(table.columns) ? table.columns.slice(0, mapViewport.maxAttributeColumns) : [];
                                mapViewport.attributeColumns = columns;
                                var fields = Array.isArray(table.fields) ? table.fields.slice(0, mapViewport.maxAttributeColumns) : columns.map(function(name) { return {name: name, type: "unknown"}; });
                                mapViewport.attributeFieldHints = fields;
                                var pageNumber = Number(table.page);
                                var pageSize = Number(table.pageSize);
                                var totalRecords = Number(table.total);
                                mapViewport.attributePage = isFinite(pageNumber) && pageNumber >= 0 ? Math.floor(pageNumber) : 0;
                                mapViewport.attributePageSize = isFinite(pageSize) && pageSize > 0 ? Math.min(mapViewport.maxAttributeRows, Math.floor(pageSize)) : 0;
                                mapViewport.attributeTotal = isFinite(totalRecords) && totalRecords >= 0 ? Math.floor(totalRecords) : 0;
                                var rows = Array.isArray(table.rows) ? table.rows.slice(0, mapViewport.maxAttributeRows) : [];
                                // Replace one bounded page at once instead of
                                // emitting one ListModel change per record.
                                attributeModel.rows = attributeDialog.visible ? rows : [];
                            } else {
                                attributeModel.rows = [];
                                mapViewport.attributeFieldHints = [];
                            }
                        }
                        if (mapCanvas.postGISCatalogPayloadGeneration !== mapViewport.postGISCatalogPayloadSeen) {
                            mapViewport.postGISCatalogPayloadSeen = mapCanvas.postGISCatalogPayloadGeneration;
                            if (postGISTableDialog.visible) {
                                var catalog = mapViewport.parseJSONObject(mapCanvas.postGISCatalogPayload || "{}");
                                if (!Array.isArray(catalog.tables))
                                    catalog.tables = [];
                                mapViewport.postGISTables = catalog.tables || [];
                                postGISTableDialog.errorText = catalog.error || "";
                                postGISTableDialog.catalogTruncated = catalog.truncated === true;
                            } else {
                                mapViewport.postGISTables = [];
                                postGISTableDialog.errorText = "";
                            }
                        }
                        if (mapCanvas.layerTreePayloadGeneration !== mapViewport.layerTreePayloadGenerationSeen) {
                            mapViewport.layerTreePayloadGenerationSeen = mapCanvas.layerTreePayloadGeneration;
                            var layers = [];
                            try {
                                layers = JSON.parse(mapCanvas.layerTreePayload || "[]");
                                if (!Array.isArray(layers))
                                    layers = [];
                            } catch (error) {
                                // Keep the synchronizer alive when a native or
                                // driver error produces a truncated payload.
                                // The next generation can repopulate the tree.
                                layers = [];
                            }
                            var previousActiveLayer = mapViewport.activeLayer;
                            mapViewport.replaceLayerModel(layers);
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
                            if (mapCanvas.visible) {
                                var labels = mapViewport.parseJSONArray(mapCanvas.layerLabelPayload || "[]");
                                // Replace the array once so a large label payload does
                                // not emit one ListModel change per row on the GUI thread.
                                mapLabelModel.rows = labels;
                            } else {
                                mapLabelModel.rows = [];
                            }
                        }
                        if (mapCanvas.vertexHandleGeneration !== mapViewport.vertexHandleGenerationSeen) {
                            mapViewport.vertexHandleGenerationSeen = mapCanvas.vertexHandleGeneration;
                            if (rootWindow.vertexEditMode && mapCanvas.visible) {
                                var handles = mapViewport.parseJSONArray(mapCanvas.vertexHandlePayload || "[]");
                                vertexHandleModel.rows = handles;
                            } else {
                                vertexHandleModel.rows = [];
                            }
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
                                metadata = mapViewport.parseJSONObject(mapCanvas.mapMetadataPayload);
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

                Rectangle {
                    id: progressCard
                    objectName: "mapRenderProgressCard"
                    anchors.top: parent.top
                    anchors.horizontalCenter: parent.horizontalCenter
                    anchors.topMargin: 10
                    width: Math.min(parent.width - 96, 420)
                    height: showProgress ? 58 : 42
                    z: 6
                    property bool showProgress: rootWindow.statusIsBusy(mapViewport.renderStatus)
                    property string currentStatus: String(mapViewport.renderStatus || "").toLowerCase()
                    property bool hasRenderProblem: currentStatus.indexOf("incomplete") >= 0 ||
                                                   currentStatus.indexOf("render error") >= 0 ||
                                                   currentStatus.indexOf("render stopped") >= 0 ||
                                                   currentStatus.indexOf("render ready:") === 0
                    visible: layerModel.count > 0 && (showProgress || hasRenderProblem)
                    color: currentStatus.indexOf("incomplete") >= 0 || currentStatus.indexOf("render error") >= 0 || currentStatus.indexOf("render stopped") >= 0
                           ? "#fff7f5" : hasRenderProblem ? "#fffaf0" : "#f8fbfd"
                    border.color: hasRenderProblem ? rootWindow.statusColor(mapViewport.renderStatus) : "#bfd0dc"
                    radius: 7
                    property var progressSnapshot: rootWindow.renderProgressSnapshot(mapViewport.renderStatus)
                    ColumnLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 12
                        anchors.rightMargin: 12
                        anchors.topMargin: 7
                        anchors.bottomMargin: 7
                        spacing: 4
                        Label {
                            Layout.fillWidth: true
                            text: rootWindow.localizedStatus(mapViewport.renderStatus)
                            color: progressCard.hasRenderProblem ? rootWindow.statusColor(mapViewport.renderStatus) : "#24516d"
                            elide: Text.ElideRight
                            font.bold: progressCard.hasRenderProblem
                            font.pixelSize: 12
                        }
                        ProgressBar {
                            objectName: "mapRenderProgressBar"
                            Layout.fillWidth: true
                            implicitHeight: 7
                            visible: progressCard.showProgress
                            from: 0
                            to: 100
                            value: progressCard.progressSnapshot.value
                            indeterminate: !progressCard.progressSnapshot.known
                        }
                    }
                }

                Column {
                    anchors.top: parent.top
                    anchors.right: parent.right
                    anchors.margins: 10
                    z: 6
                    visible: layerModel.count > 0
                    spacing: 4
                    Button {
                        objectName: "zoomInButton"
                        width: 38
                        height: 34
                        enabled: mapViewport.mapZoom < mapViewport.maxMapZoom() - 0.000001
                        text: "+"
                        font.pixelSize: 20
                        Accessible.name: rootWindow.language === "ko" ? "확대" : rootWindow.language === "jp" ? "拡大" : "Zoom in"
                        ToolTip.visible: hovered
                        ToolTip.text: Accessible.name
                        onClicked: mapViewport.zoomByFactor(1.5)
                    }
                    Button {
                        objectName: "zoomOutButton"
                        width: 38
                        height: 34
                        enabled: mapViewport.mapZoom > 0.250001
                        text: "−"
                        font.pixelSize: 20
                        Accessible.name: rootWindow.language === "ko" ? "축소" : rootWindow.language === "jp" ? "縮小" : "Zoom out"
                        ToolTip.visible: hovered
                        ToolTip.text: Accessible.name
                        onClicked: mapViewport.zoomByFactor(1 / 1.5)
                    }
                    Button {
                        objectName: "zoomToFullExtentButton"
                        width: 38
                        height: 34
                        text: "⤢"
                        Accessible.name: rootWindow.tr("Zoom to full extent")
                        ToolTip.visible: hovered
                        ToolTip.text: Accessible.name
                        onClicked: mapViewport.zoomToFullExtent()
                    }
                }
            }
        }
    }

    Dialog {
        id: applicationSettingsDialog
        objectName: "applicationSettingsDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.language === "ko" ? "GoGIS 설정" : rootWindow.language === "jp" ? "GoGIS 設定" : "GoGIS Settings"
        width: Math.min(480, rootWindow.width - 48)
        standardButtons: Dialog.Close
        contentItem: ColumnLayout {
            spacing: 12
            Label {
                Layout.fillWidth: true
                text: rootWindow.language === "ko" ? "앱 기본값" : rootWindow.language === "jp" ? "アプリの基本設定" : "Application defaults"
                font.bold: true
            }
            RowLayout {
                Layout.fillWidth: true
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.language === "ko" ? "표시 언어" : rootWindow.language === "jp" ? "表示言語" : "Interface language"
                }
                ComboBox {
                    objectName: "interfaceLanguageCombo"
                    textRole: "label"
                    valueRole: "value"
                    model: [
                        { label: rootWindow.language === "ko" ? "시스템 기본값" : rootWindow.language === "jp" ? "システム設定" : "System default", value: "system" },
                        { label: "한국어", value: "ko" },
                        { label: "English", value: "en" },
                        { label: "日本語", value: "jp" }
                    ]
                    currentIndex: Math.max(0, indexOfValue(userSettings.interfaceLanguage))
                    onCurrentValueChanged: userSettings.interfaceLanguage = currentValue
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.language === "ko" ? "Shapefile 자동 공간 인덱스 기준" : rootWindow.language === "jp" ? "Shapefile自動空間インデックス基準" : "Automatic Shapefile index threshold"
                }
                SpinBox {
                    objectName: "shapefileIndexThresholdSpinBox"
                    from: 0
                    to: 1000000
                    stepSize: 1000
                    editable: true
                    value: userSettings.shapefileIndexThreshold
                    textFromValue: function(value, locale) { return value === 0 ? (rootWindow.language === "ko" ? "사용 안 함" : rootWindow.language === "jp" ? "無効" : "Off") : Number(value).toLocaleString(locale, 'f', 0) }
                    valueFromText: function(text, locale) { return Number.fromLocaleString(locale, text.replace(/[^0-9]/g, "")) }
                    onValueModified: userSettings.shapefileIndexThreshold = value
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.language === "ko" ? "화면 피처 상한" : rootWindow.language === "jp" ? "画面の地物数上限" : "Visible feature limit"
                }
                SpinBox {
                    objectName: "visibleFeatureLimitSpinBox"
                    from: 50000
                    to: 1000000
                    stepSize: 50000
                    editable: true
                    value: userSettings.visibleFeatureLimit
                    textFromValue: function(value, locale) { return Number(value).toLocaleString(locale, 'f', 0) }
                    valueFromText: function(text, locale) { return Number.fromLocaleString(locale, text.replace(/[^0-9]/g, "")) }
                    onValueModified: {
                        userSettings.visibleFeatureLimit = value;
                        mapViewport.viewportGeneration += 1;
                    }
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.language === "ko" ? "화면 데이터 예산 (MiB)" : rootWindow.language === "jp" ? "画面データの上限 (MiB)" : "Viewport data budget (MiB)"
                }
                SpinBox {
                    objectName: "visiblePayloadLimitSpinBox"
                    from: 0
                    to: 4096
                    stepSize: 128
                    editable: true
                    value: userSettings.visiblePayloadLimitMiB
                    textFromValue: function(value, locale) {
                        return value === 0 ? (rootWindow.language === "ko" ? "제한 없음" : rootWindow.language === "jp" ? "無制限" : "Unlimited") : Number(value).toLocaleString(locale, 'f', 0)
                    }
                    valueFromText: function(text, locale) {
                        var digits = text.replace(/[^0-9]/g, "");
                        return digits === "" ? 0 : Number.fromLocaleString(locale, digits);
                    }
                    onValueModified: {
                        userSettings.visiblePayloadLimitMiB = value;
                        mapViewport.viewportGeneration += 1;
                    }
                }
            }
            Label {
                Layout.fillWidth: true
                wrapMode: Text.WordWrap
                color: "#65717d"
                text: rootWindow.language === "ko" ? "화면 피처 상한과 데이터 예산은 즉시 적용됩니다. 데이터 예산은 분할 조회 결과를 합친 청크에도 적용됩니다. 0은 추정 예산을 해제하며, 개별 조회는 32 MiB로 제한됩니다. 실제 메모리 사용량은 별개입니다. Shapefile 인덱스 기준은 다음 실행부터 적용됩니다." : rootWindow.language === "jp" ? "画面の地物数上限とデータ上限はすぐに適用され、分割取得を結合したチャンクにも適用されます。0では推定上限が無効になります。個別の取得は32 MiBまでです。実際のメモリ使用量は別です。Shapefileインデックス基準は次回起動時から適用されます。" : "The visible feature limit and data budget apply immediately, including chunks merged from split queries. Zero disables the estimated budget; each source query stays limited to 32 MiB. Actual memory use is separate. The Shapefile index threshold applies on next launch."
            }
            Button {
                objectName: "resetApplicationSettingsButton"
                text: rootWindow.language === "ko" ? "기본값으로 초기화" : rootWindow.language === "jp" ? "既定値に戻す" : "Restore defaults"
                onClicked: {
                    userSettings.interfaceLanguage = "system";
                    userSettings.shapefileIndexThreshold = 10000;
                    userSettings.visibleFeatureLimit = 500000;
                    userSettings.visiblePayloadLimitMiB = 512;
                    mapViewport.viewportGeneration += 1;
                }
            }
        }
    }

    Dialog {
        id: aboutDialog
        objectName: "aboutDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
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
        anchors.centerIn: Overlay.overlay
        title: rootWindow.tr("Attributes") + " — " + mapViewport.activeLayer
        width: Math.min(780, rootWindow.width - 48)
        height: Math.min(680, rootWindow.height - 80)
        standardButtons: Dialog.Close
        onOpened: {
            // Attribute rows are only needed while this dialog is visible.
            // Invalidate the gate so the active payload is restored lazily.
            mapCanvas.attributePayloadVisible = true;
            // The native bridge releases the previous JSON while the dialog
            // is closed. Request the active layer's first page again on reopen.
            mapCanvas.attributePage = 0;
            mapCanvas.attributePageGeneration += 1;
            mapViewport.attributePayloadGenerationSeen = -1;
        }
        onClosed: {
            mapCanvas.attributePayloadVisible = false;
            mapCanvas.attributePayload = "[]";
            attributeModel.rows = [];
            mapViewport.attributeColumns = [];
            mapViewport.attributePage = 0;
            mapViewport.attributePageSize = 0;
            mapViewport.attributeTotal = 0;
            // Prevent the idle synchronizer from immediately rebuilding rows
            // while the dialog is closed; a later open invalidates this gate.
            mapViewport.attributePayloadGenerationSeen = mapCanvas.attributePayloadGeneration;
        }
        contentItem: ColumnLayout {
            spacing: 8
                ListView {
                    id: attributeLayerTabs
                    objectName: "attributeLayerTabs"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 40
                    clip: true
                    orientation: ListView.Horizontal
                    reuseItems: true
                    cacheBuffer: 0
                    model: layerModel
                    delegate: TabButton {
                        required property string name
                        required property string displayName
                        required property int index
                        width: implicitWidth
                        checked: ListView.isCurrentItem
                        text: displayName === "" ? name : displayName
                        onClicked: {
                            ListView.view.currentIndex = index;
                            mapViewport.selectLayer(name);
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
                Label {
                    objectName: "attributeReadOnlyNotice"
                    Layout.fillWidth: true
                    visible: !mapViewport.activeLayerEditable() && mapViewport.activeLayer !== ""
                    text: rootWindow.language === "ko" ? "대용량 레이어는 화면 영역만 읽는 읽기 전용 모드입니다. 레코드 편집은 작은 테이블에서 사용할 수 있습니다." : rootWindow.language === "jp" ? "大容量レイヤーは表示範囲だけを読む読み取り専用モードです。レコード編集は小さいテーブルで利用できます。" : "Large layers use bounded read-only viewport mode. Record editing is available for smaller tables."
                    color: "#8a4b08"
                    wrapMode: Text.WordWrap
                }
                TextField {
                    id: propertyEditor
                    Layout.fillWidth: true
                    visible: mapViewport.selectedFeature !== ""
                    readOnly: !mapViewport.activeLayerEditable()
                    placeholderText: rootWindow.tr("Feature name")
                    text: mapViewport.editorValue
                    onTextChanged: if (activeFocus)
                        mapViewport.editorValue = text
                }
                RowLayout {
                    visible: mapViewport.selectedFeature !== ""
                    Button {
                        text: rootWindow.tr("Save")
                        enabled: mapViewport.activeLayerEditable()
                        onClicked: {
                            mapCanvas.editAction = "commit";
                            mapCanvas.editValue = mapViewport.editorValue;
                            mapCanvas.editGeneration += 1;
                        }
                    }
                    Button {
                        text: rootWindow.tr("Cancel")
                        enabled: mapViewport.activeLayerEditable()
                        onClicked: {
                            mapCanvas.editAction = "rollback";
                            mapCanvas.editValue = "";
                            mapCanvas.editGeneration += 1;
                            mapViewport.editorValue = mapCanvas.selectionValue;
                        }
                    }
                    Button {
                        objectName: "deleteSelectedRecordButton"
                        text: rootWindow.language === "ko" ? "레코드 삭제…" : rootWindow.language === "jp" ? "レコード削除…" : "Delete record…"
                        enabled: mapViewport.activeLayerGeometryEditable()
                        onClicked: deleteRecordDialog.open()
                    }
                }
                Button {
                    objectName: "addRecordButton"
                    Layout.fillWidth: true
                    text: rootWindow.language === "ko" ? "레코드 추가…" : rootWindow.language === "jp" ? "レコード追加…" : "Add record…"
                    enabled: mapViewport.activeLayer !== "" && mapViewport.activeLayerGeometryEditable()
                    onClicked: addRecordDialog.open()
                }
                GridLayout {
                    columns: 2
                    visible: mapViewport.selectedFeature !== ""
                    Label {
                        text: rootWindow.tr("Layer")
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.selectedLayer
                    }
                    Label {
                        text: rootWindow.tr("Feature")
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.selectedFeature
                    }
                    Label {
                        text: rootWindow.tr("Editable")
                        font.bold: true
                    }
                    Label {
                        text: mapViewport.activeLayerEditable() ? rootWindow.tr("Yes") : rootWindow.tr("Read-only")
                        color: mapViewport.activeLayerEditable() ? "#2e7d32" : "#65717d"
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
                        text: rootWindow.tr("Page") + " " + (mapViewport.attributePage + 1) + " / " + Math.max(1, Math.ceil(mapViewport.attributeTotal / mapViewport.attributePageSize))
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
                    objectName: "attributeRowsView"
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    visible: layerModel.count > 0 && mapViewport.attributeTotal > 0
                    model: attributeModel.rows
                    clip: true
                    reuseItems: true
                    cacheBuffer: 0
                    spacing: 6
                    delegate: Rectangle {
                        required property var modelData
                        id: featureCard
                        width: ListView.view.width
                        property var row: modelData || ({})
                        property var rowValues: row.values || ({})
                        property var rowFeatureID: row.featureId
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
                                    width: parent ? parent.width : 0
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
        implicitHeight: 86
        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 5
            spacing: 4
            Rectangle {
                Layout.fillWidth: true
                Layout.preferredHeight: 40
                color: "#eaf2f6"
                radius: 5
                RowLayout {
                    anchors.fill: parent
                    anchors.leftMargin: 12
                    anchors.rightMargin: 12
                    spacing: 16
                    ColumnLayout {
                        Layout.preferredWidth: 145
                        spacing: 0
                        Label {
                            text: rootWindow.language === "ko" ? "좌표계" : rootWindow.language === "jp" ? "座標系" : "CRS"
                            color: "#526779"
                            font.pixelSize: 10
                        }
                        Label {
                            objectName: "crsStatusLabel"
                            text: mapViewport.dataCRS || "—"
                            color: "#173c53"
                            font.pixelSize: 14
                            font.bold: true
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }
                    }
                    ColumnLayout {
                        Layout.fillWidth: true
                        Layout.minimumWidth: 250
                        spacing: 0
                        Label {
                            text: rootWindow.language === "ko" ? "현재 좌표" : rootWindow.language === "jp" ? "現在座標" : "Current coordinates"
                            color: "#526779"
                            font.pixelSize: 10
                        }
                        Label {
                            objectName: "coordinateStatusLabel"
                            text: mapViewport.statusCoordinateText()
                            color: "#173c53"
                            font.pixelSize: 14
                            font.bold: true
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                            ToolTip.visible: coordinateStatusMouse.containsMouse
                            ToolTip.text: text
                            MouseArea { id: coordinateStatusMouse; anchors.fill: parent; hoverEnabled: true }
                        }
                    }
                    ColumnLayout {
                        Layout.preferredWidth: 165
                        spacing: 0
                        Label {
                            text: rootWindow.language === "ko" ? "축척" : rootWindow.language === "jp" ? "縮尺" : "Scale"
                            color: "#526779"
                            font.pixelSize: 10
                        }
                        Label {
                            objectName: "scaleStatusLabel"
                            text: mapViewport.currentScaleText()
                            color: "#173c53"
                            font.pixelSize: 14
                            font.bold: true
                            ToolTip.visible: scaleStatusMouse.containsMouse
                            ToolTip.text: rootWindow.language === "ko" ? "96 DPI 기준의 근사 축척입니다." : rootWindow.language === "jp" ? "96 DPI に基づく概算縮尺です。" : "Approximate scale based on 96 DPI."
                            MouseArea { id: scaleStatusMouse; anchors.fill: parent; hoverEnabled: true }
                        }
                    }
                }
            }
            RowLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: 8
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
                    Layout.fillWidth: true
                    Layout.minimumWidth: 80
                    elide: Text.ElideRight
                    ToolTip.visible: renderStatusMouse.containsMouse
                    ToolTip.text: text
                    MouseArea { id: renderStatusMouse; anchors.fill: parent; hoverEnabled: true }
                }
                Button {
                    objectName: "openCoordinateNavigationButton"
                    text: rootWindow.language === "ko" ? "좌표 이동" : rootWindow.language === "jp" ? "座標へ移動" : "Go to coordinates"
                    onClicked: coordinateNavigationDialog.open()
                }
                Button {
                    objectName: "cancelWorkButton"
                    text: mapViewport.renderStatus.indexOf("Importing CSV") === 0 ? (rootWindow.language === "ko" ? "CSV 가져오기 취소" : "Cancel CSV import") : rootWindow.tr("Cancel loading/render")
                    enabled: rootWindow.statusIsBusy(mapViewport.renderStatus)
                    visible: enabled
                    onClicked: mapCanvas.cancelGeneration += 1
                }
                Label {
                    objectName: "layerCountStatusLabel"
                    text: (rootWindow.language === "ko" ? "레이어 " : rootWindow.language === "jp" ? "レイヤー " : "Layers ") + layerModel.count
                    color: "#65717d"
                }
                Label {
                    objectName: "memoryStatusLabel"
                    visible: rootWindow.width >= 1150
                    text: rootWindow.memorySummary(mapCanvas.processMemoryBytes, mapCanvas.goHeapBytes, mapCanvas.memoryStatusAvailable, mapCanvas.processMemoryKind)
                    color: "#65717d"
                    font.pixelSize: 11
                    Layout.maximumWidth: 260
                    elide: Text.ElideRight
                    ToolTip.visible: memoryMouse.containsMouse
                    ToolTip.text: language === "ko" ? "프로세스 메모리에는 Go/GDAL/Qt가 포함됩니다. GPU 메모리는 포함되지 않습니다. macOS CGO 데스크톱은 현재 RSS를 표시합니다." : language === "jp" ? "プロセスメモリにはGo/GDAL/Qtを含みます。GPUメモリは含みません。macOSのCGOデスクトップは現在のRSSを表示します。" : "Process memory includes Go/GDAL/Qt, but excludes GPU memory. The macOS CGO desktop shows current RSS."
                    MouseArea { id: memoryMouse; anchors.fill: parent; hoverEnabled: true }
                }
            }
        }
    }

    Dialog {
        id: coordinateNavigationDialog
        objectName: "coordinateNavigationDialog"
        title: rootWindow.language === "ko" ? "좌표 이동" : rootWindow.language === "jp" ? "座標へ移動" : "Go to coordinates"
        modal: true
        standardButtons: Dialog.Close
        width: Math.min(rootWindow.width - 40, 360)
        anchors.centerIn: parent
        onOpened: {
            var center = mapViewport.centerMapCoordinateValues();
            coordinateXInput.text = center ? center[0].toFixed(4) : "";
            coordinateYInput.text = center ? center[1].toFixed(4) : "";
            coordinateNavigationStatus.text = "";
        }
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                text: mapViewport.dataCRS || "—"
                color: "#526779"
            }
            TextField {
                id: coordinateXInput
                objectName: "coordinateXInput"
                Layout.fillWidth: true
                placeholderText: rootWindow.tr("X coordinate")
                onAccepted: if (mapViewport.goToCoordinate()) coordinateNavigationDialog.close()
            }
            TextField {
                id: coordinateYInput
                objectName: "coordinateYInput"
                Layout.fillWidth: true
                placeholderText: rootWindow.tr("Y coordinate")
                onAccepted: if (mapViewport.goToCoordinate()) coordinateNavigationDialog.close()
            }
            Label {
                id: coordinateNavigationStatus
                objectName: "coordinateNavigationStatus"
                Layout.fillWidth: true
                color: "#b42318"
                wrapMode: Text.WordWrap
                visible: text !== ""
            }
            Button {
                objectName: "coordinateGoButton"
                text: rootWindow.tr("Go")
                Layout.alignment: Qt.AlignRight
                onClicked: if (mapViewport.goToCoordinate()) coordinateNavigationDialog.close()
            }
        }
    }

    Dialog {
        id: layerSettingsDialog
        objectName: "layerSettingsDialog"
        modal: true
        anchors.centerIn: Overlay.overlay
        title: rootWindow.tr("Layer properties") + " — " + targetLayerName
        width: Math.min(720, rootWindow.width - 48)
        height: Math.max(320, Math.min(760, rootWindow.height - 80))
        footer: RowLayout {
            spacing: 8
            Item { Layout.fillWidth: true }
            Button {
                objectName: "layerSettingsCancelButton"
                text: rootWindow.tr("Cancel")
                onClicked: layerSettingsDialog.reject()
            }
            Button {
                objectName: "layerSettingsApplyButton"
                text: rootWindow.language === "ko" ? "적용" : rootWindow.language === "jp" ? "適用" : "Apply"
                onClicked: layerSettingsDialog.applied()
            }
        }
        property string targetLayerName: ""
        property string originalSourcePath: ""
        property string originalSourceLayerName: ""
        property string originalSourceEncoding: ""
        property string originalSourceCRS: ""
        property string labelLuaSource: ""
        property string displayRuleSource: ""
        property string activeCategory: "general"
        property string targetGeometryType: ""
        property var labelPlacementValues: ["center", "vertical", "free-angle", "center-rotated"]
        property var pointLabelPlacementValues: ["N", "NE", "E", "SE", "S", "SW", "W", "NW"]
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

        function pickColor(field) {
            layerColorPicker.targetField = field;
            layerColorPicker.selectedColor = field.text;
            layerColorPicker.open();
        }

        function loadLayer() {
            if (layerModel.count === 0)
                return;
            var rowIndex = mapViewport.findLayerIndex(targetLayerName);
            if (rowIndex < 0)
                return;
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
            sourceCRSField.text = layer.sourceCrs || "";
            originalSourceCRS = sourceCRSField.text.trim();
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
            labelPlacementField.currentIndex = Math.max(0, labelPlacementValues.indexOf(labels.placement || "center"));
            labelRotationField.text = labels.rotationField || "";
            pointLabelPlacementField.currentIndex = Math.max(0, pointLabelPlacementValues.indexOf(labels.pointPlacement || "NE"));
            pointLabelOffsetField.text = String(labels.pointOffsetMm === undefined ? 1.5 : labels.pointOffsetMm);
            labelHeightField.text = String(labels.heightMm || 2.5);
            labelMinScaleField.text = String(labels.minScale || "");
            labelMaxScaleField.text = String(labels.maxScale || "");
            labelRuleField.text = labels.rule || "";
            labelLuaSource = labels.luaScript || "";
            displayRuleSource = layer.displayRule || "";
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
                sourceCrs: sourceCRSField.text.trim(),
                visible: visibleField.checked,
                displayRule: displayRuleSource,
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
                    placement: labelPlacementValues[labelPlacementField.currentIndex],
                    rotationField: labelPlacementField.currentIndex === 3 ? labelRotationField.text : "",
                    pointPlacement: pointLabelPlacementValues[pointLabelPlacementField.currentIndex],
                    pointOffsetMm: Number(pointLabelOffsetField.text || 0),
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

        onOpened: {
            loadLayer();
            // Labels and expressions use the same bounded schema payload as
            // the attribute table. Request it while layer properties is open
            // so field hints are available without opening another dialog.
            mapCanvas.attributePayloadVisible = true;
            mapCanvas.attributePage = 0;
            mapCanvas.attributePageGeneration += 1;
            mapViewport.attributePayloadGenerationSeen = -1;
        }
        onClosed: {
            if (!attributeDialog.visible) {
                mapCanvas.attributePayloadVisible = false;
                mapCanvas.attributePayload = "[]";
                mapViewport.attributeFieldHints = [];
                mapViewport.attributePayloadGenerationSeen = mapCanvas.attributePayloadGeneration;
            }
        }
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
                ColumnLayout {
                    Layout.fillWidth: true
                    visible: layerSettingsDialog.activeCategory === "source"
                    Label {
                        text: rootWindow.language === "ko" ? "원본 좌표계 (선택적 재정의)" : rootWindow.language === "jp" ? "ソース座標系 (任意の上書き)" : "Source CRS (optional override)"
                        color: "#65717d"
                    }
                    TextField {
                        id: sourceCRSField
                        objectName: "sourceCRSField"
                        Layout.fillWidth: true
                        placeholderText: "Auto-detect, or EPSG:5186"
                    }
                    Label {
                        Layout.fillWidth: true
                        text: rootWindow.language === "ko" ? "좌표계가 없거나 잘못 감지된 경우에만 지정하세요. 잘못된 좌표계를 지정하면 위치가 크게 어긋날 수 있습니다." : rootWindow.language === "jp" ? "検出されない、または誤検出された場合のみ指定してください。誤った座標系は位置ずれの原因になります。" : "Set this only when detection is missing or incorrect. A wrong CRS can place the layer far from its true location."
                        wrapMode: Text.WordWrap
                        color: "#65717d"
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
                    visible: layerSettingsDialog.activeCategory === "source" && (sourcePathField.text.trim() !== layerSettingsDialog.originalSourcePath || sourceLayerField.text.trim() !== layerSettingsDialog.originalSourceLayerName || sourceEncodingField.editText.trim() !== layerSettingsDialog.originalSourceEncoding || sourceCRSField.text.trim() !== layerSettingsDialog.originalSourceCRS)
                    text: rootWindow.language === "ko" ? "원본 경로, 레이어, 좌표계 또는 인코딩을 바꾸면 레이어를 다시 불러옵니다. 먼저 저장하지 않은 피처 편집 내용을 저장하세요." : rootWindow.language === "jp" ? "ソース、レイヤー、座標系、文字コードを変更すると再読み込みします。未保存の編集を先に保存してください。" : "Changing the source path, layer, CRS, or encoding reloads this layer. Save unsaved feature edits in it first."
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
                    RowLayout {
                        Layout.fillWidth: true
                        visible: layerSettingsDialog.styleAppliesTo("point")
                        TextField {
                            id: pointColorField
                            objectName: "pointColorField"
                            Layout.fillWidth: true
                            readOnly: true
                        }
                        Rectangle {
                            Layout.preferredWidth: 24; Layout.preferredHeight: 24
                            color: pointColorField.text
                            border.color: "#65717d"
                            radius: 3
                            MouseArea { anchors.fill: parent; onClicked: layerSettingsDialog.pickColor(pointColorField) }
                        }
                        Button {
                            objectName: "pointColorPickerButton"
                            text: rootWindow.language === "ko" ? "색 선택…" : rootWindow.language === "jp" ? "色を選択…" : "Choose…"
                            onClicked: layerSettingsDialog.pickColor(pointColorField)
                        }
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
                    RowLayout {
                        Layout.fillWidth: true
                        visible: layerSettingsDialog.styleAppliesTo("line")
                        TextField {
                            id: lineColorField
                            objectName: "lineColorField"
                            Layout.fillWidth: true
                            readOnly: true
                        }
                        Rectangle {
                            Layout.preferredWidth: 24; Layout.preferredHeight: 24
                            color: lineColorField.text
                            border.color: "#65717d"
                            radius: 3
                            MouseArea { anchors.fill: parent; onClicked: layerSettingsDialog.pickColor(lineColorField) }
                        }
                        Button {
                            objectName: "lineColorPickerButton"
                            text: rootWindow.language === "ko" ? "색 선택…" : rootWindow.language === "jp" ? "色を選択…" : "Choose…"
                            onClicked: layerSettingsDialog.pickColor(lineColorField)
                        }
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
                    RowLayout {
                        Layout.fillWidth: true
                        visible: layerSettingsDialog.styleAppliesTo("polygon")
                        TextField {
                            id: polygonColorField
                            objectName: "polygonColorField"
                            Layout.fillWidth: true
                            readOnly: true
                        }
                        Rectangle {
                            Layout.preferredWidth: 24; Layout.preferredHeight: 24
                            color: polygonColorField.text
                            border.color: "#65717d"
                            radius: 3
                            MouseArea { anchors.fill: parent; onClicked: layerSettingsDialog.pickColor(polygonColorField) }
                        }
                        Button {
                            objectName: "polygonColorPickerButton"
                            text: rootWindow.language === "ko" ? "색 선택…" : rootWindow.language === "jp" ? "色を選択…" : "Choose…"
                            onClicked: layerSettingsDialog.pickColor(polygonColorField)
                        }
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
                Label {
                    objectName: "labelVisibilityHint"
                    Layout.fillWidth: true
                    wrapMode: Text.WordWrap
                    color: "#65717d"
                    visible: layerSettingsDialog.activeCategory === "labels" && labelsEnabledField.checked
                    text: {
                        var count = 0;
                        var availableCount = 0;
                        var denominator = mapViewport.currentScaleDenominator();
                        for (var i = 0; i < mapLabelModel.count; ++i) {
                            var label = mapLabelModel.get(i);
                            if (label.layer === layerSettingsDialog.targetLayerName) {
                                ++count;
                                if ((label.minScale <= 0 || denominator >= label.minScale) && (label.maxScale <= 0 || denominator <= label.maxScale))
                                    ++availableCount;
                            }
                        }
                        for (var layerIndex = 0; layerIndex < layerModel.count; ++layerIndex) {
                            var layerRow = layerModel.get(layerIndex);
                            if (layerRow.name === layerSettingsDialog.targetLayerName && !layerRow.layerVisible)
                                return rootWindow.language === "ko" ? "레이어가 숨겨져 있어 레이블도 표시되지 않습니다. 레이어 표시를 켜세요." : rootWindow.language === "jp" ? "レイヤーが非表示のためラベルも表示されません。レイヤーを表示してください。" : "The layer is hidden, so its labels are hidden too. Turn on layer visibility.";
                        }
                        if (availableCount > 0)
                            return rootWindow.language === "ko" ? "현재 화면과 축척에서 표시 가능한 레이블 " + availableCount + "개입니다." : rootWindow.language === "jp" ? "現在の表示範囲と縮尺で表示可能なラベルは " + availableCount + " 件です。" : availableCount + " labels are available at the current view and scale.";
                        if (count > 0)
                            return rootWindow.language === "ko" ? "현재 축척이 레이블의 최소/최대 축척 범위 밖입니다. 축척 값을 확인하세요." : rootWindow.language === "jp" ? "現在の縮尺はラベルの最小/最大範囲外です。縮尺設定を確認してください。" : "The current scale is outside the labels' minimum/maximum scale range. Check those limits.";
                        // Go excludes out-of-scale labels before decluttering, so
                        // an empty model can still mean a configured scale limit.
                        for (var scaleIndex = 0; scaleIndex < layerModel.count; ++scaleIndex) {
                            var scaleRow = layerModel.get(scaleIndex);
                            if (scaleRow.name !== layerSettingsDialog.targetLayerName)
                                continue;
                            var configured = scaleRow.labels || ({});
                            if ((configured.minScale > 0 && denominator < configured.minScale) ||
                                    (configured.maxScale > 0 && denominator > configured.maxScale))
                                return rootWindow.language === "ko" ? "현재 축척이 레이블의 최소/최대 축척 범위 밖입니다. 축척 값을 확인하세요." : rootWindow.language === "jp" ? "現在の縮尺はラベルの最小/最大範囲外です。縮尺設定を確認してください。" : "The current scale is outside the labels' minimum/maximum scale range. Check those limits.";
                            break;
                        }
                        return rootWindow.language === "ko" ? "현재 표시 중인 레이블이 없습니다. 겹침 방지로 생략됐을 수 있으니 확대하거나 레이블 필드·축척 범위를 확인하세요. DXF 내보내기는 이 표시 생략을 적용하지 않습니다." : rootWindow.language === "jp" ? "表示中のラベルはありません。重なり防止で省略された可能性があります。拡大するかフィールド・縮尺範囲を確認してください。DXF出力には表示上の省略を適用しません。" : "No labels are displayed. Zoom in or check the field and scale range; collision removal may hide them. DXF export keeps every eligible label.";
                    }
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
                        onClicked: {
                            labelFieldHintPopup.targetEditor = null;
                            labelFieldHintPopup.open();
                        }
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
                        model: rootWindow.language === "ko" ?
                                   ["동–서 방향 수평", "남–북 방향 수평", "자유롭게 (가장 긴 선분 각도)", "속성 필드 각도 (기존 설정)"] :
                               rootWindow.language === "jp" ?
                                   ["東西方向・水平", "南北方向・垂直", "自由角度（最長線分）", "属性フィールドの角度（従来）"] :
                                   ["East–west horizontal", "North–south vertical", "Free angle (longest segment)", "Attribute rotation (legacy)"]
                    }
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    visible: layerSettingsDialog.activeCategory === "labels" && labelPlacementField.currentIndex === 3
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
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    visible: layerSettingsDialog.activeCategory === "labels" && layerSettingsDialog.styleAppliesTo("point")
                    Label {
                        text: rootWindow.tr("Point label position")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    ComboBox {
                        id: pointLabelPlacementField
                        objectName: "pointLabelPlacementField"
                        Layout.fillWidth: true
                        model: rootWindow.language === "ko" ?
                                   ["위", "오른쪽 위", "오른쪽", "오른쪽 아래", "아래", "왼쪽 아래", "왼쪽", "왼쪽 위"] :
                               rootWindow.language === "jp" ?
                                   ["上", "右上", "右", "右下", "下", "左下", "左", "左上"] :
                                   ["Top", "Top right", "Right", "Bottom right", "Bottom", "Bottom left", "Left", "Top left"]
                    }
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    visible: layerSettingsDialog.activeCategory === "labels" && layerSettingsDialog.styleAppliesTo("point")
                    Label {
                        text: rootWindow.tr("Point label offset (mm)")
                        Layout.preferredWidth: layerSettingsDialog.labelCaptionWidth
                    }
                    TextField {
                        id: pointLabelOffsetField
                        objectName: "pointLabelOffsetField"
                        Layout.fillWidth: true
                        inputMethodHints: Qt.ImhFormattedNumbersOnly
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
                Label {
                    Layout.fillWidth: true
                    text: rootWindow.tr("Feature display filter hides features from the map only; source data stays unchanged.")
                    wrapMode: Text.WordWrap
                    color: "#65717d"
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
        property var targetEditor: null
        property var matchingFields: mapViewport.attributeFieldHints.filter(function(field) {
            var query = fieldSearch.text.trim().toLowerCase();
            return query === "" || String(field.name).toLowerCase().indexOf(query) >= 0 ||
                   String(field.type || "").toLowerCase().indexOf(query) >= 0;
        })
        onOpened: fieldSearch.text = ""
        onClosed: targetEditor = null
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.tr("Fields for this layer")
                font.bold: true
            }
            Label {
                Layout.fillWidth: true
                text: labelFieldHintPopup.targetEditor ?
                    (rootWindow.language === "ko" ? "필드를 선택하면 편집기 커서 위치에 삽입됩니다." : rootWindow.language === "jp" ? "フィールドを選ぶとエディターのカーソル位置に挿入します。" : "Select a field to insert it at the editor cursor.") :
                    rootWindow.tr("Click a field to insert ${FIELD} at the cursor. A single field name also works.")
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            TextField {
                id: fieldSearch
                objectName: "labelFieldSearch"
                Layout.fillWidth: true
                placeholderText: rootWindow.language === "ko" ? "필드 이름 또는 형식 검색" : rootWindow.language === "jp" ? "フィールド名または型を検索" : "Search field name or type"
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
                reuseItems: true
                cacheBuffer: 0
                model: labelFieldHintPopup.matchingFields
                spacing: 4
                delegate: Button {
                    required property var modelData
                    objectName: "labelFieldHintRow"
                    width: labelFieldList.width
                    text: modelData.name + "  ·  " + (modelData.type || "unknown")
                    onClicked: {
                        if (labelFieldHintPopup.targetEditor) {
                            var editor = labelFieldHintPopup.targetEditor;
                            var access = mapViewport.luaFieldAccess(modelData.name);
                            var position = editor.cursorPosition;
                            editor.insert(position, access);
                            editor.cursorPosition = position + access.length;
                        } else {
                            layerSettingsDialog.insertLabelField(modelData.name);
                        }
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
        anchors.centerIn: Overlay.overlay
        title: rootWindow.tr("Lua editor") + " — " + layerSettingsDialog.targetLayerName
        width: Math.min(760, rootWindow.width - 64)
        height: Math.min(620, rootWindow.height - 64)
        footer: RowLayout {
            spacing: 8
            Item { Layout.fillWidth: true }
            Button {
                objectName: "luaEditorCancelButton"
                text: rootWindow.tr("Cancel")
                onClicked: luaEditorDialog.reject()
            }
            Button {
                objectName: "luaEditorOkButton"
                text: rootWindow.language === "ko" ? "확인" : rootWindow.language === "jp" ? "確認" : "OK"
                onClicked: luaEditorDialog.accept()
            }
        }
        property bool initializing: false
        property bool saved: false
        property string originalLabelRule: ""
        property string originalLabelLua: ""
        property string originalDisplayRule: ""
        function storeDraft() {
            if (initializing)
                return;
            labelRuleField.text = labelRuleEditor.text;
            layerSettingsDialog.labelLuaSource = labelLuaField.text;
            layerSettingsDialog.displayRuleSource = featureDisplayRuleEditor.text;
        }
        onOpened: {
            initializing = true;
            saved = false;
            originalLabelRule = labelRuleField.text;
            originalLabelLua = layerSettingsDialog.labelLuaSource;
            originalDisplayRule = layerSettingsDialog.displayRuleSource;
            luaEditorTabs.currentIndex = 0;
            luaHelpButton.checked = false;
            labelRuleEditor.text = labelRuleField.text;
            labelLuaField.text = layerSettingsDialog.labelLuaSource;
            featureDisplayRuleEditor.text = layerSettingsDialog.displayRuleSource;
            initializing = false;
        }
        onAccepted: {
            storeDraft();
            saved = true;
            layerSettingsDialog.submitLayer();
        }
        onClosed: {
            if (!saved) {
                labelRuleField.text = originalLabelRule;
                layerSettingsDialog.labelLuaSource = originalLabelLua;
                layerSettingsDialog.displayRuleSource = originalDisplayRule;
            }
        }
        function activeEditor() {
            if (luaEditorTabs.currentIndex === 0) return featureDisplayRuleEditor;
            if (luaEditorTabs.currentIndex === 1) return labelRuleEditor;
            return labelLuaField;
        }
        contentItem: ColumnLayout {
            spacing: 10
            TabBar {
                id: luaEditorTabs
                objectName: "luaEditorTabs"
                Layout.fillWidth: true
                onCurrentIndexChanged: luaEditorDialog.storeDraft()
                TabButton { text: rootWindow.tr("Display filter") }
                TabButton { text: rootWindow.tr("Label rule") }
                TabButton { text: rootWindow.tr("Label text") }
            }
            Label {
                Layout.fillWidth: true
                text: luaEditorTabs.currentIndex === 0
                    ? rootWindow.tr("Feature display filter — return true to draw this feature")
                    : luaEditorTabs.currentIndex === 1
                        ? rootWindow.tr("Display rule — return true to show this feature's label")
                        : rootWindow.tr("Label text — return string, number, or nil")
                wrapMode: Text.WordWrap
                color: "#45515c"
            }
            Label {
                Layout.fillWidth: true
                visible: luaEditorTabs.currentIndex === 0
                text: rootWindow.tr("Feature display filter hides features from the map only; source data stays unchanged.")
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            Item {
                Layout.fillWidth: true
                Layout.fillHeight: true
                LuaCodeEditor {
                    id: featureDisplayRuleEditor
                    objectName: "featureDisplayRuleEditor"
                    anchors.fill: parent
                    visible: luaEditorTabs.currentIndex === 0
                    placeholderText: "return feature.STATUS ~= \"retired\""
                }
                LuaCodeEditor {
                    id: labelRuleEditor
                    objectName: "labelRuleEditor"
                    anchors.fill: parent
                    visible: luaEditorTabs.currentIndex === 1
                    placeholderText: "return feature.CLASS == \"primary\""
                }
                LuaCodeEditor {
                    id: labelLuaField
                    objectName: "labelLuaField"
                    anchors.fill: parent
                    visible: luaEditorTabs.currentIndex === 2
                    placeholderText: "return string.format(\"%s (%s)\", feature.NAME, feature.CLASS)"
                }
            }
            Label {
                objectName: "luaFieldSummary"
                Layout.fillWidth: true
                text: mapViewport.luaFieldHintText()
                color: "#65717d"
                elide: Text.ElideRight
            }
            RowLayout {
                Layout.fillWidth: true
                Button {
                    objectName: "luaBrowseFieldsButton"
                    text: rootWindow.tr("Available fields…")
                    enabled: mapViewport.attributeFieldHints.length > 0
                    onClicked: {
                        labelFieldHintPopup.targetEditor = luaEditorDialog.activeEditor();
                        labelFieldHintPopup.open();
                    }
                }
                Button {
                    id: luaHelpButton
                    objectName: "luaHelpButton"
                    text: rootWindow.tr("Lua helpers")
                    checkable: true
                }
                Item { Layout.fillWidth: true }
                Button {
                    objectName: "luaExamplesButton"
                    text: rootWindow.tr("Examples")
                    onClicked: luaExamplesMenu.open()
                    Menu {
                        id: luaExamplesMenu
                        objectName: "luaExamplesMenu"
                        MenuItem {
                            text: rootWindow.tr("Insert filter example")
                            visible: luaEditorTabs.currentIndex === 0
                            onTriggered: featureDisplayRuleEditor.text = "return feature.STATUS ~= \"retired\""
                        }
                        MenuItem {
                            objectName: "insertLuaLikeExampleButton"
                            text: rootWindow.tr("Insert LIKE example")
                            visible: luaEditorTabs.currentIndex === 0
                            onTriggered: featureDisplayRuleEditor.text = "return gogis.contains(feature.NAME, \"road\", true)"
                        }
                        MenuItem {
                            text: rootWindow.tr("Insert rule example")
                            visible: luaEditorTabs.currentIndex === 1
                            onTriggered: labelRuleEditor.text = "return feature.CLASS == \"primary\""
                        }
                        MenuItem {
                            text: rootWindow.tr("Insert label example")
                            visible: luaEditorTabs.currentIndex === 2
                            onTriggered: labelLuaField.text = "return string.format(\"%s (%s)\", feature.NAME, feature.CLASS)"
                        }
                        MenuItem {
                            objectName: "insertLuaConcatenationExampleButton"
                            text: rootWindow.tr("Insert concatenation example")
                            visible: luaEditorTabs.currentIndex === 2
                            onTriggered: labelLuaField.text = "return tostring(gogis.coalesce(feature.NAME, \"\")) .. \" / \" .. tostring(gogis.coalesce(feature.CODE, \"\"))"
                        }
                        MenuItem {
                            objectName: "insertLuaNumberFormatExampleButton"
                            text: rootWindow.tr("Insert number format example")
                            visible: luaEditorTabs.currentIndex === 2
                            onTriggered: labelLuaField.text = "return gogis.format_number(feature.AREA, 2)"
                        }
                    }
                }
            }
            Label {
                objectName: "luaShortcutHelp"
                Layout.fillWidth: true
                text: rootWindow.tr("Scripts run once for each feature. The read-only `feature` table exposes the layer's attributes. Use feature.FIELD or feature[\"FIELD NAME\"] for field names with spaces.")
                wrapMode: Text.WordWrap
                color: "#65717d"
            }
            Label {
                objectName: "luaHelperDetails"
                Layout.fillWidth: true
                visible: luaHelpButton.checked
                text: "gogis.format_number(value, decimals) · gogis.coalesce(a, b) · gogis.trim(text) · gogis.contains(text, part, ignoreCase)"
                wrapMode: Text.WordWrap
                color: "#526578"
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
                        if (!diagnosticLogDialog.visible)
                            return "";
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

    QuickDialogs.FileDialog {
        id: csvOpenDialog
        title: "Open CSV/TSV"
        fileMode: QuickDialogs.FileDialog.OpenFiles
        nameFilters: ["Delimited text (*.csv *.tsv)", "All files (*)"]
        onAccepted: {
            var paths = [];
            for (var index = 0; index < selectedFiles.length; index++)
                paths.push(mapViewport.localPathFromUrl(selectedFiles[index]));
            csvPathField.text = paths.join("\n");
        }
    }

    Platform.FileDialog {
        id: workflowTableOpenDialog
        title: rootWindow.language === "ko" ? "업무대장 열기" : "Open table CSV/TSV"
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: ["Delimited text (*.csv *.tsv)", "All files (*)"]
        onAccepted: workflowTable.text = mapViewport.localPathFromUrl(file)
    }

    Platform.FileDialog {
        id: workflowValidationSaveDialog
        title: rootWindow.language === "ko" ? "검수 결과 저장" : "Save validation report"
        fileMode: Platform.FileDialog.SaveFile
        nameFilters: ["CSV (*.csv)"]
        onAccepted: {
            var path = mapViewport.localPathFromUrl(file);
            workflowOutputPath.text = path.toLowerCase().endsWith(".csv") ? path : path + ".csv";
        }
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
        id: dxfExportDialog
        objectName: "dxfExportDialog"
        title: rootWindow.language === "ko" ? "DXF 저장 위치" : rootWindow.language === "jp" ? "DXF保存先" : "Save DXF"
        fileMode: Platform.FileDialog.SaveFile
        nameFilters: ["DXF (*.dxf)"]
        property string selectedProfile: "ares-utf8"
        property string selectedOptions: ""
        onAccepted: {
            var path = mapViewport.localPathFromUrl(file);
            if (!path.toLowerCase().endsWith(".dxf"))
                path += ".dxf";
            mapCanvas.savePath = path;
            mapCanvas.saveProfile = selectedProfile;
            mapCanvas.saveOptions = selectedOptions;
            mapCanvas.saveGeneration += 1;
        }
    }

    Dialog {
        id: dxfEncodingDialog
        objectName: "dxfEncodingDialog"
        modal: true
        title: rootWindow.language === "ko" ? "DXF 내보내기 설정" : rootWindow.language === "jp" ? "DXF書き出し設定" : "DXF export settings"
        standardButtons: Dialog.Ok | Dialog.Cancel
        width: Math.min(640, rootWindow.width - 48)
        anchors.centerIn: Overlay.overlay
        property string validationMessage: ""
        ListModel { id: dxfLayerOptions; objectName: "dxfLayerOptionsModel" }
        onOpened: {
            dxfEncodingChoice.currentIndex = 0;
            // Rebuild from the current layer tree on every open. Keeping the
            // first snapshot caused removed layers and old export settings to
            // survive subsequent layer edits.
            dxfLayerOptions.clear();
            for (var i = 0; i < layerModel.count; ++i) {
                var layer = layerModel.get(i);
                dxfLayerOptions.append({name: layer.name,
                    cadName: layer.displayName || layer.name,
                    included: true, geometryEnabled: true, labelsEnabled: true,
                    geometryType: layer.geometryType || ""});
            }
            validationMessage = "";
        }
        onClosed: dxfLayerOptions.clear()
        onAccepted: {
            var plan = [];
            var seen = {};
            var included = 0;
            for (var i = 0; i < dxfLayerOptions.count; ++i) {
                var option = dxfLayerOptions.get(i);
                var cadName = option.cadName.trim();
                if (option.included) {
                    included++;
                    if (cadName === "" || (!option.geometryEnabled && !option.labelsEnabled)) {
                        validationMessage = rootWindow.language === "ko" ? "선택한 레이어에는 CAD 이름과 도형/레이블 중 하나가 필요합니다." : "Selected layers need a CAD name and geometry or labels.";
                        Qt.callLater(function() { dxfEncodingDialog.open(); });
                        return;
                    }
                    if (cadName.length > 255 || /[<>\/\\\":;?*|=,']/.test(cadName)) {
                        validationMessage = rootWindow.language === "ko" ? "CAD 레이어 이름에 허용되지 않는 문자 또는 길이가 있습니다: " + cadName : "Invalid CAD layer name: " + cadName;
                        Qt.callLater(function() { dxfEncodingDialog.open(); });
                        return;
                    }
                    var key = cadName.toUpperCase();
                    if (seen[key]) {
                        validationMessage = rootWindow.language === "ko" ? "CAD 레이어 이름이 중복됩니다: " + cadName : "Duplicate CAD layer name: " + cadName;
                        Qt.callLater(function() { dxfEncodingDialog.open(); });
                        return;
                    }
                    seen[key] = true;
                }
                plan.push({name: option.name, cadName: cadName, include: option.included,
                    geometry: option.geometryEnabled, labels: option.labelsEnabled});
            }
            if (included === 0) {
                validationMessage = rootWindow.language === "ko" ? "내보낼 레이어를 하나 이상 선택하세요." : "Select at least one layer.";
                Qt.callLater(function() { dxfEncodingDialog.open(); });
                return;
            }
            dxfExportDialog.selectedProfile = dxfEncodingChoice.currentValue;
            dxfExportDialog.selectedOptions = JSON.stringify(plan);
            dxfExportDialog.open();
        }
        contentItem: ColumnLayout {
            spacing: 8
            Label {
                Layout.fillWidth: true
                text: rootWindow.language === "ko" ? "문자 손실 없이 열 수 있도록 대상 CAD에 맞는 인코딩을 선택하세요." : rootWindow.language === "jp" ? "文字化けを避けるため、対象CADに合った文字コードを選択してください。" : "Choose the encoding supported by the target CAD application to avoid text corruption."
                wrapMode: Text.WordWrap
            }
            ComboBox {
                id: dxfEncodingChoice
                objectName: "dxfEncodingChoice"
                Layout.fillWidth: true
                textRole: "label"
                valueRole: "profile"
                model: rootWindow.language === "ko" ? [
                    {label: "CP949 (한국어 CAD)", profile: "ares-cp949"},
                    {label: "UTF-8", profile: "ares-utf8"},
                    {label: "Shift-JIS (일본어 CAD)", profile: "ares-shift-jis"}
                ] : rootWindow.language === "jp" ? [
                    {label: "Shift-JIS (日本語CAD)", profile: "ares-shift-jis"},
                    {label: "UTF-8", profile: "ares-utf8"},
                    {label: "CP949 (韓国語CAD)", profile: "ares-cp949"}
                ] : [
                    {label: "UTF-8", profile: "ares-utf8"},
                    {label: "CP949 (Korean CAD)", profile: "ares-cp949"},
                    {label: "Shift-JIS (Japanese CAD)", profile: "ares-shift-jis"}
                ]
            }
            Label {
                Layout.fillWidth: true
                text: rootWindow.language === "ko" ? "2D 도형: 점→POINT(원형 십자 표시), 선·폴리곤 경계→LWPOLYLINE. 채우기가 설정된 폴리곤은 SOLID 삼각형으로 채웁니다. Z 좌표는 제외하며 레이블은 같은 CAD 레이어의 TEXT입니다." : rootWindow.language === "jp" ? "2D図形: 点→POINT（円と十字の記号）、線・ポリゴン境界→LWPOLYLINE。塗り設定のあるポリゴンはSOLID三角形で塗ります。Z座標は除外し、ラベルは同じCADレイヤーのTEXTです。" : "2D geometry: points → POINT with a circle-and-cross marker; lines and polygon outlines → LWPOLYLINE. Filled polygons use SOLID triangles. Z coordinates are omitted; labels become TEXT on the same CAD layer."
                wrapMode: Text.WordWrap
            }
            ListView {
                id: dxfLayerOptionsView
                objectName: "dxfLayerOptionsView"
                Layout.fillWidth: true
                Layout.preferredHeight: Math.min(340, Math.max(100, dxfLayerOptions.count * 115))
                clip: true
                reuseItems: true
                cacheBuffer: 0
                model: dxfLayerOptions
                delegate: ColumnLayout {
                    width: dxfLayerOptionsView.width
                    CheckBox {
                        text: model.cadName + " (" + model.geometryType + ")"
                        checked: model.included
                        onClicked: dxfLayerOptions.setProperty(index, "included", checked)
                    }
                    Label {
                        Layout.fillWidth: true
                        leftPadding: 20
                        color: "#526578"
                        text: rootWindow.language === "ko"
                            ? "도형 해석: " + (/point/i.test(model.geometryType) ? "점 → POINT" : /polygon/i.test(model.geometryType) ? "폴리곤 경계 → LWPOLYLINE" : /line/i.test(model.geometryType) ? "선 → LWPOLYLINE" : "원본 형상에 따라 POINT/LWPOLYLINE")
                            : "Geometry mapping: " + (/point/i.test(model.geometryType) ? "points → POINT" : /polygon/i.test(model.geometryType) ? "polygon outlines → LWPOLYLINE" : /line/i.test(model.geometryType) ? "lines → LWPOLYLINE" : "POINT/LWPOLYLINE by source shape")
                    }
                    RowLayout {
                        Layout.fillWidth: true
                        enabled: model.included
                        Label { text: rootWindow.language === "ko" ? "CAD 이름" : "CAD name" }
                        TextField {
                            Layout.fillWidth: true
                            text: model.cadName
                            // Do not write the initial binding value back to
                            // the ListModel. On macOS the native control can
                            // emit textChanged while the delegate is being
                            // constructed, which used to erase CAD names.
                            onTextChanged: if (activeFocus) dxfLayerOptions.setProperty(index, "cadName", text)
                        }
                    }
                    RowLayout {
                        enabled: model.included
                        CheckBox {
                            text: rootWindow.language === "ko" ? "도형" : "Geometry"
                            checked: model.geometryEnabled
                            onClicked: dxfLayerOptions.setProperty(index, "geometryEnabled", checked)
                        }
                        CheckBox {
                            text: rootWindow.language === "ko" ? "레이블" : "Labels"
                            checked: model.labelsEnabled
                            onClicked: dxfLayerOptions.setProperty(index, "labelsEnabled", checked)
                        }
                    }
                }
            }
            Label {
                Layout.fillWidth: true
                visible: dxfEncodingDialog.validationMessage !== ""
                color: "#b42318"
                text: dxfEncodingDialog.validationMessage
                wrapMode: Text.WordWrap
            }
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
        onAccepted: {
            sourcePathField.text = mapViewport.localPathFromUrl(file);
            sourceCRSField.text = "";
        }
    }
}
