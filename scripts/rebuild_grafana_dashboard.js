const fs = require('fs');
const path = require('path');

const dashPath = path.join(__dirname, '..', 'docs', 'grafana_telemetry.json');
const dash = JSON.parse(fs.readFileSync(dashPath, 'utf8'));

// Helper map of existing panels by ID
const panelsById = {};
dash.panels.forEach(p => {
  panelsById[p.id] = p;
});

// Update title, version, annotations if needed
dash.title = "WarLink • Stockholm GPN Telemetry";
dash.version = (dash.version || 1) + 1;

// Define helper panel creators
function createRow(id, title, y) {
  return {
    collapsed: false,
    gridPos: { h: 1, w: 24, x: 0, y },
    id,
    title,
    type: "row"
  };
}

// 1. Storage breakdown panel (id: 401)
const storagePanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Детализация использования дискового пространства накопителя NVMe ключевыми компонентами и сервисами WarLink.",
  fieldConfig: {
    defaults: {
      color: { fixedColor: "#FF5E1F", mode: "fixed" },
      min: 0,
      unit: "bytes",
      mappings: [
        {
          options: {
            "postgres": { text: "База данных PostgreSQL", index: 0 },
            "victoriametrics": { text: "Метрики VictoriaMetrics", index: 1 },
            "system_logs": { text: "Системный журнал (Journald)", index: 2 },
            "avatars": { text: "Кэш аватарок пользователей", index: 3 }
          },
          type: "value"
        }
      ]
    },
    overrides: []
  },
  gridPos: { h: 8, w: 12, x: 12, y: 44 },
  id: 401,
  options: {
    barRadius: 0.1,
    barWidth: 0.65,
    groupWidth: 0.7,
    legend: { displayMode: "list", placement: "bottom", showLegend: false },
    orientation: "horizontal",
    showValue: "always",
    stacking: "none",
    tooltip: { mode: "single", sort: "desc" },
    xField: "Компонент",
    xTickLabelRotation: 0
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_storage_component_bytes",
      format: "table",
      instant: true,
      range: false,
      refId: "A"
    }
  ],
  title: "Детализация дискового накопителя (Размер компонентов)",
  transformations: [
    {
      id: "organize",
      options: {
        excludeByName: { Time: true, __name__: true, instance: true, job: true },
        indexByName: { Value: 1, component: 0 },
        renameByName: { Value: "Занято места", component: "Компонент" }
      }
    }
  ],
  type: "barchart"
};

// 2. Classes popularity panel (id: 501)
const classesPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Суммарная прокачка и распределение уровней участников сообщества по 6 игровым классам WARDOGS.",
  fieldConfig: {
    defaults: {
      color: { fixedColor: "#FF5E1F", mode: "fixed" },
      min: 0,
      unit: "short"
    },
    overrides: []
  },
  gridPos: { h: 9, w: 8, x: 0, y: 53 },
  id: 501,
  options: {
    barRadius: 0.1,
    barWidth: 0.65,
    groupWidth: 0.7,
    legend: { displayMode: "list", placement: "bottom", showLegend: false },
    orientation: "horizontal",
    showValue: "always",
    stacking: "none",
    tooltip: { mode: "single", sort: "desc" },
    xField: "Класс",
    xTickLabelRotation: 0
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_progression_role_level_sum",
      format: "table",
      instant: true,
      range: false,
      refId: "A"
    }
  ],
  title: "Популярность классов (Суммарный ранг)",
  transformations: [
    {
      id: "organize",
      options: {
        excludeByName: { Time: true, __name__: true, instance: true, job: true, role: true },
        indexByName: { Value: 1, role_name: 0 },
        renameByName: { Value: "Суммарный ранг", role_name: "Класс" }
      }
    },
    {
      id: "sortBy",
      options: {
        fields: {},
        sort: [
          {
            desc: true,
            field: "Суммарный ранг"
          }
        ]
      }
    }
  ],
  type: "barchart"
};

// 3. Career level & community stats (id: 502)
const careerStatsPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Общая статистика зрелости базы игроков WARDOGS: средний и рекордный уровни карьеры.",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#22c55e", value: null },
          { color: "#FF5E1F", value: 50 }
        ]
      },
      unit: "short"
    },
    overrides: [
      {
        matcher: { id: "byName", options: "Средний уровень карьеры" },
        properties: [{ id: "displayName", value: "Средний уровень" }, { id: "unit", value: "short" }]
      },
      {
        matcher: { id: "byName", options: "Рекордный ранг карьеры" },
        properties: [{ id: "displayName", value: "Максимальный ранг" }, { id: "unit", value: "short" }]
      },
      {
        matcher: { id: "byName", options: "Игроков с прогрессией" },
        properties: [{ id: "displayName", value: "Синхронизировано" }, { id: "unit", value: "short" }]
      }
    ]
  },
  gridPos: { h: 9, w: 4, x: 8, y: 53 },
  id: 502,
  options: {
    colorMode: "value",
    graphMode: "none",
    justifyMode: "center",
    orientation: "vertical",
    reduceOptions: { calcs: ["lastNotNull"], fields: "", values: false },
    textMode: "value_and_name",
    wideLayout: false
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_progression_career_level_avg",
      instant: true,
      legendFormat: "Средний уровень карьеры",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_progression_career_level_max",
      instant: true,
      legendFormat: "Рекордный ранг карьеры",
      refId: "B"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_progression_players_total",
      instant: true,
      legendFormat: "Игроков с прогрессией",
      refId: "C"
    }
  ],
  title: "Ранг и охват прогрессии",
  type: "stat"
};

// 4. Wishlist top-10 items table (id: 503)
const wishlistTablePanel = {
  datasource: { type: "yesoreyeram-infinity-datasource", uid: "warlink-infinity" },
  description: "Самое востребованное сообществом оружие, модули и обвесы WARDOGS, к открытию которых стремятся игроки в приложении.",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      custom: {
        align: "auto",
        cellOptions: { type: "auto" },
        inspect: false
      },
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "text", value: null },
          { color: "#FF5E1F", value: 2 }
        ]
      }
    },
    overrides: [
      {
        matcher: { id: "byName", options: "name_ru" },
        properties: [{ id: "custom.width", value: 240 }, { id: "displayName", value: "Желанный предмет" }]
      },
      {
        matcher: { id: "byName", options: "category_ru" },
        properties: [{ id: "custom.width", value: 140 }, { id: "displayName", value: "Категория" }]
      },
      {
        matcher: { id: "byName", options: "count" },
        properties: [
          { id: "custom.width", value: 100 },
          { id: "displayName", value: "Игроков хотят" },
          {
            id: "custom.cellOptions",
            value: { mode: "gradient", type: "gauge" }
          }
        ]
      },
      {
        matcher: { id: "byName", options: "item_id" },
        properties: [{ id: "custom.width", value: 160 }, { id: "displayName", value: "ID предмета" }]
      }
    ]
  },
  gridPos: { h: 9, w: 12, x: 12, y: 53 },
  id: 503,
  options: {
    cellHeight: "sm",
    footer: { countRows: false, fields: "", reducer: ["sum"], show: false },
    showHeader: true,
    sortBy: [{ desc: true, displayName: "Игроков хотят" }]
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      columns: [
        { selector: "name_ru", text: "name_ru", type: "string" },
        { selector: "category_ru", text: "category_ru", type: "string" },
        { selector: "count", text: "count", type: "number" },
        { selector: "item_id", text: "item_id", type: "string" }
      ],
      datasource: { type: "yesoreyeram-infinity-datasource", uid: "warlink-infinity" },
      format: "table",
      global_query_id: "",
      refId: "A",
      root_selector: "top_wishlist",
      source: "url",
      type: "json",
      url: "http://127.0.0.1:8081/api/v1/analytics/progression",
      url_options: { method: "GET" }
    }
  ],
  title: "Топ-10 желаемых разблокировок (Wishlist)",
  type: "table"
};

// 5. Session playtime panel (id: 701)
const playtimePanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Средняя и максимальная продолжительность активных игровых сессий клиентов шлюза в минутах.",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#22c55e", value: null },
          { color: "#FF5E1F", value: 60 }
        ]
      },
      unit: "m"
    },
    overrides: [
      {
        matcher: { id: "byName", options: "Среднее время сессии" },
        properties: [{ id: "displayName", value: "Средняя сессия" }]
      },
      {
        matcher: { id: "byName", options: "Рекордная сессия" },
        properties: [{ id: "displayName", value: "Макс. сессия" }]
      }
    ]
  },
  gridPos: { h: 4, w: 6, x: 0, y: 75 },
  id: 701,
  options: {
    colorMode: "value",
    graphMode: "none",
    justifyMode: "center",
    orientation: "horizontal",
    reduceOptions: { calcs: ["lastNotNull"], fields: "", values: false },
    textMode: "value_and_name",
    wideLayout: false
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_session_duration_avg_minutes",
      instant: true,
      legendFormat: "Среднее время сессии",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_session_duration_max_minutes",
      instant: true,
      legendFormat: "Рекордная сессия",
      refId: "B"
    }
  ],
  title: "Длительность игровой сессии (Playtime)",
  type: "stat"
};

// 6. Client versions online panel (id: 703)
const clientVersionsPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Распределение используемых версий приложения WarLink среди подключенных в данный момент игроков.",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#22c55e", value: null },
          { color: "#FF5E1F", value: 1 }
        ]
      },
      unit: "short"
    },
    overrides: []
  },
  gridPos: { h: 4, w: 6, x: 12, y: 75 },
  id: 703,
  options: {
    colorMode: "value",
    graphMode: "none",
    justifyMode: "center",
    orientation: "horizontal",
    reduceOptions: { calcs: ["lastNotNull"], fields: "", values: false },
    textMode: "value_and_name",
    wideLayout: false
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_client_version_online",
      instant: true,
      legendFormat: "{{version}}",
      refId: "A"
    }
  ],
  title: "Версии клиентов онлайн",
  type: "stat"
};

// 7. Session rejections total panel (id: 704)
const rejectionsPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Суммарное число сессий, отклоненных защитой сервера (лимит запросов, лимит IP или неверная подпись).",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#22c55e", value: null },
          { color: "#f59e0b", value: 5 },
          { color: "#ef4444", value: 20 }
        ]
      },
      unit: "short"
    },
    overrides: []
  },
  gridPos: { h: 4, w: 6, x: 18, y: 75 },
  id: 704,
  options: {
    colorMode: "value",
    graphMode: "none",
    justifyMode: "center",
    orientation: "horizontal",
    reduceOptions: { calcs: ["lastNotNull"], fields: "", values: false },
    textMode: "value_and_name",
    wideLayout: false
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum(warlink_session_rejections_total)",
      instant: true,
      legendFormat: "Всего отклонено",
      refId: "A"
    }
  ],
  title: "Отклонённые попытки входа (Защита)",
  type: "stat"
};

// 8. Service health status panel (id: 901)
const serviceHealthPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Мониторинг жизнеспособности системных демонов и сервисов WarLink (1 = в норме, 0 = сбой).",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      mappings: [
        {
          options: {
            "0": { color: "#ef4444", text: "СБОЙ", index: 0 },
            "1": { color: "#22c55e", text: "В НОРМЕ", index: 1 }
          },
          type: "value"
        }
      ],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#ef4444", value: null },
          { color: "#22c55e", value: 1 }
        ]
      }
    },
    overrides: [
      { matcher: { id: "byName", options: "api" }, properties: [{ id: "displayName", value: "WarLink API" }] },
      { matcher: { id: "byName", options: "hysteria" }, properties: [{ id: "displayName", value: "Hysteria 2" }] },
      { matcher: { id: "byName", options: "postgres" }, properties: [{ id: "displayName", value: "PostgreSQL DB" }] },
      { matcher: { id: "byName", options: "redis" }, properties: [{ id: "displayName", value: "Redis Store" }] },
      { matcher: { id: "byName", options: "victoriametrics" }, properties: [{ id: "displayName", value: "VictoriaMetrics" }] },
      { matcher: { id: "byName", options: "nginx" }, properties: [{ id: "displayName", value: "Nginx Proxy" }] }
    ]
  },
  gridPos: { h: 7, w: 12, x: 0, y: 98 },
  id: 901,
  options: {
    colorMode: "background",
    graphMode: "none",
    justifyMode: "center",
    orientation: "horizontal",
    reduceOptions: { calcs: ["lastNotNull"], fields: "", values: false },
    textMode: "value_and_name",
    wideLayout: false
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_service_status",
      instant: true,
      legendFormat: "{{service}}",
      refId: "A"
    }
  ],
  title: "Статус системных служб сервера",
  type: "stat"
};

// 9. API HTTP status codes panel (id: 902)
const apiHttpCodesPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Статистика интенсивности и классов ответов HTTP API WarLink (2xx успех, 4xx ошибки запросов, 5xx серверные сбои).",
  fieldConfig: {
    defaults: {
      color: { mode: "palette-classic" },
      custom: {
        axisBorderShow: false,
        axisCenteredZero: false,
        axisColorMode: "text",
        axisLabel: "Запросов",
        axisPlacement: "auto",
        drawStyle: "line",
        fillOpacity: 10,
        gradientMode: "none",
        lineInterpolation: "smooth",
        lineWidth: 2,
        pointSize: 5,
        showPoints: "never",
        spanNulls: false,
        stacking: { group: "A", mode: "none" }
      },
      unit: "short"
    },
    overrides: [
      { matcher: { id: "byName", options: "2xx Успешные" }, properties: [{ id: "color", value: { fixedColor: "#22c55e", mode: "fixed" } }] },
      { matcher: { id: "byName", options: "4xx Ошибки клиента" }, properties: [{ id: "color", value: { fixedColor: "#eab308", mode: "fixed" } }] },
      { matcher: { id: "byName", options: "5xx Сбои сервера" }, properties: [{ id: "color", value: { fixedColor: "#ef4444", mode: "fixed" } }] }
    ]
  },
  gridPos: { h: 7, w: 12, x: 12, y: 98 },
  id: 902,
  options: {
    legend: { calcs: ["lastNotNull"], displayMode: "table", placement: "bottom", showLegend: true },
    tooltip: { mode: "multi", sort: "desc" }
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_api_http_requests_total{code=\"2xx\"}",
      legendFormat: "2xx Успешные",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_api_http_requests_total{code=\"4xx\"}",
      legendFormat: "4xx Ошибки клиента",
      refId: "B"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_api_http_requests_total{code=\"5xx\"}",
      legendFormat: "5xx Сбои сервера",
      refId: "C"
    }
  ],
  title: "Запросы к API по классам ответов (HTTP 2xx / 4xx / 5xx)",
  type: "timeseries"
};

// 10. Update UDP panel 41 with kernel buffer error metrics
if (panelsById[41]) {
  panelsById[41].title = "Ошибки и переполнение буферов UDP (Kernel Buffer Drops)";
  panelsById[41].description = "Прямой индикатор переполнения сокетов приема/отправки ядра Linux (RcvbufErrors, SndbufErrors, InErrors). Нулевые значения гарантируют отсутствие отбрасывания игровых UDP-пакетов шлюзом.";
  panelsById[41].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_udp_rcvbuf_errors_total",
      legendFormat: "Переполнение буфера приема (RcvbufErrors)",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_udp_sndbuf_errors_total",
      legendFormat: "Переполнение буфера отправки (SndbufErrors)",
      refId: "B"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_udp_in_errors_total",
      legendFormat: "Ошибочные входящие пакеты (InErrors)",
      refId: "C"
    }
  ];
}

// 11. Update UDP intensity panel 42
if (panelsById[42]) {
  panelsById[42].title = "Интенсивность потока пакетов в секунду (Packets / Sec)";
  panelsById[42].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "irate(warlink_udp_in_datagrams_total[1m])",
      legendFormat: "UDP входящие (Rx Pkts/s)",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "irate(warlink_udp_out_datagrams_total[1m])",
      legendFormat: "UDP исходящие (Tx Pkts/s)",
      refId: "B"
    }
  ];
}

// 12. Update Financial Runway panel 31
if (panelsById[31]) {
  panelsById[31].title = "Гарантированный запас работы (Runway)";
  panelsById[31].description = "Расчетный срок непрерывной работы шлюза на текущем балансе хостинга без новых пожертвований.";
  panelsById[31].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_financial_runway_days",
      instant: true,
      legendFormat: "Дней работы шлюза",
      refId: "A"
    }
  ];
  panelsById[31].fieldConfig = {
    defaults: {
      color: { mode: "thresholds" },
      decimals: 0,
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#ef4444", value: null },
          { color: "#eab308", value: 30 },
          { color: "#22c55e", value: 90 },
          { color: "#FF5E1F", value: 180 }
        ]
      },
      unit: " дн."
    },
    overrides: []
  };
}

if (panelsById[2]) {
  panelsById[2].fieldConfig.defaults.unit = " дн.";
  panelsById[2].fieldConfig.defaults.decimals = 0;
}

// Reassemble the dashboard into 10 structured sections:
const newPanels = [];

// SECTION 1: Главное состояние и системные ресурсы
newPanels.push(createRow(100, "1. Главное: состояние сервера и игроки", 0));
[
  { id: 1, w: 3, h: 4, x: 0, y: 1 },
  { id: 2, w: 3, h: 4, x: 3, y: 1 },
  { id: 13, w: 3, h: 4, x: 6, y: 1 },
  { id: 11, w: 3, h: 4, x: 9, y: 1 },
  { id: 3, w: 4, h: 4, x: 12, y: 1 },
  { id: 4, w: 4, h: 4, x: 16, y: 1 },
  { id: 15, w: 4, h: 4, x: 20, y: 1 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 2: Финансы и долгосрочный прогноз
newPanels.push(createRow(107, "2. Финансы: сбор донатов и прогноз работы сервера (Runway)", 5));
[
  { id: 31, w: 6, h: 4, x: 0, y: 6 },
  { id: 28, w: 6, h: 4, x: 6, y: 6 },
  { id: 20, w: 4, h: 4, x: 12, y: 6 },
  { id: 25, w: 4, h: 4, x: 16, y: 6 },
  { id: 27, w: 4, h: 4, x: 20, y: 6 },
  { id: 29, w: 24, h: 8, x: 0, y: 10 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 3: Качество связи и здоровье UDP ядра
newPanels.push(createRow(109, "3. Качество связи: задержки, потери пакетов и буферы UDP", 18));
[
  { id: 41, w: 8, h: 8, x: 0, y: 19 },
  { id: 45, w: 8, h: 8, x: 8, y: 19 },
  { id: 42, w: 8, h: 8, x: 16, y: 19 },
  { id: 46, w: 12, h: 8, x: 0, y: 27 },
  { id: 40, w: 12, h: 8, x: 12, y: 27 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 4: Нагрузка и детализация накопителя
newPanels.push(createRow(101, "4. Нагрузка: процессор, интернет-канал и накопитель", 35));
[
  { id: 12, w: 12, h: 8, x: 0, y: 36 },
  { id: 6, w: 12, h: 8, x: 12, y: 36 },
  { id: 7, w: 12, h: 8, x: 0, y: 44 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});
newPanels.push(storagePanel);

// SECTION 5: Мета-аналитика прогрессии WARDOGS
newPanels.push(createRow(105, "5. Мета-аналитика прогрессии WARDOGS (Классы, Ранги, Wishlist)", 52));
newPanels.push(classesPanel);
newPanels.push(careerStatsPanel);
newPanels.push(wishlistTablePanel);

// SECTION 6: Географическая плотность и выбор PoP-серверов
newPanels.push(createRow(106, "6. Географическая плотность аудитории и выбор PoP-серверов", 62));
[
  { id: 120, w: 16, h: 11, x: 0, y: 63 },
  { id: 121, w: 8, h: 11, x: 16, y: 63 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 7: Аудитория, игровое время и версии клиентов
newPanels.push(createRow(108, "7. Аудитория: игровое время, активность и версии клиентов", 74));
newPanels.push(playtimePanel);
if (panelsById[24]) {
  panelsById[24].gridPos = { h: 4, w: 6, x: 6, y: 75 };
  newPanels.push(panelsById[24]);
}
newPanels.push(clientVersionsPanel);
newPanels.push(rejectionsPanel);
[
  { id: 21, w: 12, h: 8, x: 0, y: 79 },
  { id: 22, w: 12, h: 8, x: 12, y: 79 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 8: Выбор сообщества (Голосование за игры)
newPanels.push(createRow(110, "8. Выбор сообщества: голосование за новые игры", 87));
[
  { id: 50, w: 8, h: 9, x: 0, y: 88 },
  { id: 51, w: 16, h: 9, x: 8, y: 88 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 9: Инфраструктурное здоровье служб и стабильность API
newPanels.push(createRow(119, "9. Инфраструктурное здоровье служб и стабильность API", 97));
newPanels.push(serviceHealthPanel);
newPanels.push(apiHttpCodesPanel);

// SECTION 10: Инспектор активных сессий и релизы программы
newPanels.push(createRow(102, "10. Живые сессии и история релизов программы", 105));
[
  { id: 91, w: 14, h: 10, x: 0, y: 106 },
  { id: 95, w: 10, h: 10, x: 14, y: 106 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

dash.panels = newPanels;

fs.writeFileSync(dashPath, JSON.stringify(dash, null, 2), 'utf8');
console.log('Successfully reorganized dashboard into 10 sections with', dash.panels.length, 'panels!');
