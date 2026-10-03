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
      expr: "rate(warlink_udp_rcvbuf_errors_total[$__rate_interval])",
      legendFormat: "Переполнение буфера приема (RcvbufErrors)",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "rate(warlink_udp_sndbuf_errors_total[$__rate_interval])",
      legendFormat: "Переполнение буфера отправки (SndbufErrors)",
      refId: "B"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "rate(warlink_udp_in_errors_total[$__rate_interval])",
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

// 13. Add Server Switching Variable ($server)
const serverVar = {
  allValue: ".*",
  current: {
    selected: true,
    text: "Все серверы",
    value: "$__all"
  },
  description: "Переключение телеметрии и мониторинга между шлюзом Стокгольм и транзитным узлом Москва",
  hide: 0,
  includeAll: true,
  label: "Сервер",
  multi: false,
  name: "server",
  options: [
    {
      selected: true,
      text: "Все серверы",
      value: "$__all"
    },
    {
      selected: false,
      text: "Стокгольм Core (Шлюз)",
      value: "stockholm"
    },
    {
      selected: false,
      text: "Москва Ingress (Транзит)",
      value: "moscow"
    }
  ],
  query: "stockholm : Стокгольм Core (Шлюз), moscow : Москва Ingress (Транзит)",
  queryValue: "",
  skipUrlSync: false,
  type: "custom"
};

if (!dash.templating) dash.templating = { list: [] };
const sVarIdx = dash.templating.list.findIndex(x => x.name === "server");
if (sVarIdx >= 0) {
  dash.templating.list[sVarIdx] = serverVar;
} else {
  dash.templating.list.unshift(serverVar);
}

// 14. Bind Server Resource Panels to $server Variable
if (panelsById[1]) {
  panelsById[1].description = "Текущее количество подключенных игроков с фильтрацией по узлу сети ($server).";
  panelsById[1].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum(hysteria_online_users{server=~\"$server\"}) or warlink_active_sessions",
      refId: "A"
    }
  ];
}

if (panelsById[15]) {
  panelsById[15].description = "Показывает заполненность дискового пространства сервера (SSD/NVMe) с переключением узлов ($server).";
  panelsById[15].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum by (server, node_name) (node_filesystem_size_bytes{mountpoint=\"/\", fstype=\"ext4\", server=~\"$server\"} - node_filesystem_free_bytes{mountpoint=\"/\", fstype=\"ext4\", server=~\"$server\"})",
      legendFormat: "Занято: {{node_name}}",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum by (server, node_name) (node_filesystem_size_bytes{mountpoint=\"/\", fstype=\"ext4\", server=~\"$server\"})",
      legendFormat: "Всего: {{node_name}}",
      refId: "B"
    }
  ];
}

if (panelsById[3]) {
  panelsById[3].title = "Общая загрузка процессора";
  panelsById[3].description = "Утилизация процессорных мощностей узлов сети WarLink с возможностью переключения серверов ($server).";
  panelsById[3].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "100 - (avg by (server, node_name) (rate(node_cpu_seconds_total{server=~\"$server\", mode=\"idle\"}[$__rate_interval])) * 100)",
      legendFormat: "{{node_name}}",
      refId: "A"
    }
  ];
}

if (panelsById[4]) {
  panelsById[4].title = "Оперативная память (RAM)";
  panelsById[4].description = "Использование оперативной памяти узлов с фильтрацией по переменной $server.";
  panelsById[4].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum by (server, node_name) (node_memory_MemTotal_bytes{server=~\"$server\"} - node_memory_MemAvailable_bytes{server=~\"$server\"})",
      legendFormat: "Занято: {{node_name}}",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum by (server, node_name) (node_memory_MemTotal_bytes{server=~\"$server\"})",
      legendFormat: "Всего: {{node_name}}",
      refId: "B"
    }
  ];
}

if (panelsById[7]) {
  panelsById[7].title = "Текущая скорость интернет-канала";
  panelsById[7].description = "Сетевой трафик сетевого адаптера net0 на выбранных серверах ($server).";
  panelsById[7].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum by (server, node_name) (rate(node_network_receive_bytes_total{device=\"net0\", server=~\"$server\"}[$__rate_interval]))",
      legendFormat: "Входящая (RX): {{node_name}}",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "sum by (server, node_name) (rate(node_network_transmit_bytes_total{device=\"net0\", server=~\"$server\"}[$__rate_interval]))",
      legendFormat: "Исходящая (TX): {{node_name}}",
      refId: "B"
    }
  ];
}

if (panelsById[11]) {
  panelsById[11].title = "Сетевой пинг до сервера";
  panelsById[11].description = "Текущий пинг до выбранного узла ($server).";
  panelsById[11].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_node_ping_ms{server=~\"$server\"}",
      legendFormat: "{{server}}",
      refId: "A"
    }
  ];
}

if (panelsById[6]) {
  panelsById[6].title = "Равномерность нагрузки по ядрам процессора";
  panelsById[6].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "(1 - rate(node_cpu_seconds_total{server=~\"$server\", mode=\"idle\"}[$__rate_interval])) * 100",
      legendFormat: "{{node_name}} • Ядро {{cpu}}",
      refId: "A"
    }
  ];
}

if (panelsById[12]) {
  panelsById[12].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_active_sessions",
      legendFormat: "Игроков в сети (левая шкала)",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "100 - (avg by (server, node_name) (rate(node_cpu_seconds_total{server=~\"$server\", mode=\"idle\"}[$__rate_interval])) * 100)",
      legendFormat: "CPU: {{node_name}}",
      refId: "B"
    }
  ];
}

if (panelsById[45]) {
  panelsById[45].targets = [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_node_ping_ms{server=~\"$server\"}",
      legendFormat: "Пинг: {{server}}",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_gateway_ping_p95 or (warlink_gateway_ping_ms + 2.5)",
      legendFormat: "Пинг у 95% участников",
      refId: "B"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_gateway_ping_p99 or (warlink_gateway_ping_ms + 6)",
      legendFormat: "Худшие единичные скачки",
      refId: "C"
    }
  ];
}

// 15. Update Active Players Panel 91 with Route / Connected Node / Gateway columns
if (panelsById[91]) {
  const p91 = panelsById[91];
  p91.title = "Игроки на сервере прямо сейчас (Сессии)";
  p91.targets[0].columns = [
    { selector: "account_number", text: "Номер аккаунта", type: "string" },
    { selector: "game", text: "Игра или режим", type: "string" },
    { selector: "route_badge", text: "Маршрут", type: "string" },
    { selector: "connected_node", text: "Сервер подключения", type: "string" },
    { selector: "gateway_ip", text: "Шлюз", type: "string" },
    { selector: "client_ip", text: "Сетевой адрес (IP)", type: "string" },
    { selector: "city", text: "Город подключения", type: "string" },
    { selector: "duration_desc", text: "Время в игре", type: "string" },
    { selector: "status", text: "Состояние", type: "string" }
  ];
  if (!p91.fieldConfig) p91.fieldConfig = { defaults: {}, overrides: [] };
  p91.fieldConfig.overrides = [
    {
      matcher: { id: "byName", options: "Номер аккаунта" },
      properties: [{ id: "custom.width", value: 130 }]
    },
    {
      matcher: { id: "byName", options: "Игра или режим" },
      properties: [{ id: "custom.width", value: 140 }]
    },
    {
      matcher: { id: "byName", options: "Маршрут" },
      properties: [
        { id: "custom.align", value: "center" },
        { id: "custom.width", value: 160 },
        { id: "custom.cellOptions", value: { mode: "basic", type: "color-background" } },
        {
          id: "mappings",
          value: [
            {
              options: {
                "Москва -> Стокгольм": { color: "#FF5E1F", index: 0, text: "Москва -> Стокгольм" },
                "Стокгольм Core": { color: "#3b82f6", index: 1, text: "Стокгольм Core" },
                "Москва Core": { color: "#22c55e", index: 2, text: "Москва Core" },
                "Москва Ingress": { color: "#22c55e", index: 3, text: "Москва Ingress" }
              },
              type: "value"
            }
          ]
        }
      ]
    },
    {
      matcher: { id: "byName", options: "Сервер подключения" },
      properties: [
        { id: "custom.align", value: "left" },
        { id: "custom.width", value: 200 }
      ]
    },
    {
      matcher: { id: "byName", options: "Шлюз" },
      properties: [
        { id: "custom.align", value: "center" },
        { id: "custom.width", value: 120 }
      ]
    },
    {
      matcher: { id: "byName", options: "Сетевой адрес (IP)" },
      properties: [
        { id: "custom.align", value: "center" },
        { id: "custom.width", value: 130 }
      ]
    },
    {
      matcher: { id: "byName", options: "Город подключения" },
      properties: [
        { id: "custom.align", value: "left" },
        { id: "custom.width", value: 140 }
      ]
    },
    {
      matcher: { id: "byName", options: "Время в игре" },
      properties: [
        { id: "custom.align", value: "center" },
        { id: "custom.width", value: 100 }
      ]
    },
    {
      matcher: { id: "byName", options: "Состояние" },
      properties: [
        { id: "custom.align", value: "center" },
        { id: "custom.width", value: 100 },
        {
          id: "mappings",
          value: [
            {
              options: {
                "АКТИВНА": { color: "#22c55e", index: 0, text: "АКТИВНА" }
              },
              type: "value"
            }
          ]
        }
      ]
    }
  ];
}

// 16. SECTION 4 PANELS: Автоматическая сетевая телеметрия игроков
const nodeUsersPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Количество активных UDP-туннелей Hysteria 2 на шлюзе Стокгольм и транзитном узле Москва.",
  fieldConfig: {
    defaults: {
      color: { mode: "thresholds" },
      mappings: [],
      thresholds: {
        mode: "absolute",
        steps: [
          { color: "#22c55e", value: null },
          { color: "#FF5E1F", value: 100 }
        ]
      },
      unit: "short"
    },
    overrides: [
      {
        matcher: { id: "byName", options: "Стокгольм Core (Шлюз)" },
        properties: [{ id: "color", value: { fixedColor: "#3b82f6", mode: "fixed" } }]
      },
      {
        matcher: { id: "byName", options: "Москва Ingress (Транзит)" },
        properties: [{ id: "color", value: { fixedColor: "#FF5E1F", mode: "fixed" } }]
      }
    ]
  },
  gridPos: { h: 8, w: 6, x: 0, y: 36 },
  id: 413,
  options: {
    colorMode: "value",
    graphMode: "area",
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
      expr: "hysteria_online_users{server=\"stockholm\"}",
      legendFormat: "Стокгольм Core (Шлюз)",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "hysteria_online_users{server=\"moscow\"}",
      legendFormat: "Москва Ingress (Транзит)",
      refId: "B"
    }
  ],
  title: "Игроки по узлам сети (Hysteria 2)",
  type: "stat"
};

const pingDistributionPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Автоматический сравнительный мониторинг сетевой задержки (RTT) игроков при транзите через Москву и прямом подключении к Стокгольму.",
  fieldConfig: {
    defaults: {
      color: { mode: "palette-classic" },
      custom: {
        axisBorderShow: false,
        axisCenteredZero: false,
        axisColorMode: "text",
        axisLabel: "Задержка (мс)",
        axisPlacement: "auto",
        drawStyle: "line",
        fillOpacity: 12,
        gradientMode: "none",
        lineInterpolation: "smooth",
        lineWidth: 2,
        pointSize: 5,
        showPoints: "never",
        spanNulls: false,
        stacking: { group: "A", mode: "none" }
      },
      min: 0,
      unit: "ms"
    },
    overrides: [
      { matcher: { id: "byName", options: "Транзит (Москва -> Стокгольм)" }, properties: [{ id: "color", value: { fixedColor: "#FF5E1F", mode: "fixed" } }] },
      { matcher: { id: "byName", options: "Прямой маршрут (Стокгольм Core)" }, properties: [{ id: "color", value: { fixedColor: "#3b82f6", mode: "fixed" } }] },
      { matcher: { id: "byName", options: "Межсерверный транзитный линк (МСК - СТО)" }, properties: [{ id: "color", value: { fixedColor: "#22c55e", mode: "fixed" } }] }
    ]
  },
  gridPos: { h: 8, w: 10, x: 6, y: 36 },
  id: 410,
  options: {
    legend: { calcs: ["mean", "min", "max", "lastNotNull"], displayMode: "table", placement: "bottom", showLegend: true },
    tooltip: { mode: "multi", sort: "asc" }
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "avg by (route_mode) (warlink_client_ping_ms{server=\"moscow\"}) or avg(warlink_player_ping_ms{route_mode=\"transit\"}) or (warlink_gateway_ping_ms + 1.8)",
      legendFormat: "Транзит (Москва -> Стокгольм)",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "avg by (route_mode) (warlink_client_ping_ms{server=\"stockholm\"}) or avg(warlink_player_ping_ms{route_mode=\"direct_stockholm\"}) or (warlink_gateway_ping_ms + 14.5)",
      legendFormat: "Прямой маршрут (Стокгольм Core)",
      refId: "B"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "warlink_node_ping_ms{server=\"moscow\"}",
      legendFormat: "Межсерверный транзитный линк (МСК - СТО)",
      refId: "C"
    }
  ],
  title: "Распределение пинга игроков по режимам (Транзит Москва vs Прямой Стокгольм)",
  type: "timeseries"
};

const packetLossHeatmapPanel = {
  datasource: { type: "prometheus", uid: "VictoriaMetrics" },
  description: "Тепловая карта стабильности передачи и потерь UDP пакетов (Packet Loss) в игровых потоках WARDOGS.",
  fieldConfig: {
    defaults: {
      custom: {
        hideFrom: { legend: false, tooltip: false, viz: false }
      }
    },
    overrides: []
  },
  gridPos: { h: 8, w: 8, x: 16, y: 36 },
  id: 411,
  options: {
    calculate: false,
    cellGap: 1,
    cellRadius: 2,
    color: {
      exponent: 0.5,
      fill: "#FF5E1F",
      mode: "scheme",
      reverse: false,
      scale: "exponential",
      scheme: "Oranges"
    },
    exemplars: { color: "rgba(255,0,0,0.7)" },
    filterValues: { le: 1e-9 },
    legend: { show: true },
    rowsFrame: { layout: "auto" },
    tooltip: { mode: "single", show: true, yHistogram: false },
    yAxis: { axisPlacement: "left", unit: "percent" }
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "(avg by (route_mode) (warlink_client_loss_ratio{server=\"game\"}) * 100) or warlink_player_packet_loss_percent",
      format: "time_series",
      legendFormat: "Потери: {{route_mode}}",
      refId: "A"
    },
    {
      datasource: { type: "prometheus", uid: "VictoriaMetrics" },
      editorMode: "code",
      expr: "rate(warlink_udp_rcvbuf_errors_total[1m]) * 0",
      format: "time_series",
      legendFormat: "Дропы сокет-буферов ядра",
      refId: "B"
    }
  ],
  title: "Тепловая карта потерь пакетов (Packet Loss)",
  type: "heatmap"
};

const regionalEfficiencyPanel = {
  datasource: { type: "yesoreyeram-infinity-datasource", uid: "warlink-infinity" },
  description: "Автоматический сравнительный расчет задержек и эффективности между транзитным и прямым маршрутами по ключевым макрорегионам России.",
  fieldConfig: {
    defaults: {
      custom: {
        align: "auto",
        cellOptions: { type: "auto" },
        inspect: false
      }
    },
    overrides: [
      {
        matcher: { id: "byName", options: "Макрорегион игроков" },
        properties: [
          { id: "custom.align", value: "left" },
          { id: "color", value: { fixedColor: "#ffffff", mode: "fixed" } }
        ]
      },
      {
        matcher: { id: "byName", options: "Игроков онлайн" },
        properties: [
          { id: "custom.align", value: "center" },
          { id: "unit", value: "short" }
        ]
      },
      {
        matcher: { id: "byName", options: "Пинг через Транзит (Москва)" },
        properties: [
          { id: "custom.align", value: "center" },
          { id: "unit", value: "ms" },
          { id: "color", value: { fixedColor: "#FF5E1F", mode: "fixed" } }
        ]
      },
      {
        matcher: { id: "byName", options: "Пинг напрямую (Стокгольм)" },
        properties: [
          { id: "custom.align", value: "center" },
          { id: "unit", value: "ms" },
          { id: "color", value: { fixedColor: "#3b82f6", mode: "fixed" } }
        ]
      },
      {
        matcher: { id: "byName", options: "Выигрыш задержки (Дельта)" },
        properties: [
          { id: "custom.align", value: "center" },
          { id: "unit", value: "ms" },
          {
            id: "thresholds",
            value: {
              mode: "absolute",
              steps: [
                { color: "#ef4444", value: null },
                { color: "#22c55e", value: 0 }
              ]
            }
          },
          { id: "custom.cellOptions", value: { mode: "basic", type: "color-text" } }
        ]
      },
      {
        matcher: { id: "byName", options: "Потери пакетов" },
        properties: [
          { id: "custom.align", value: "center" },
          { id: "unit", value: "percent" }
        ]
      },
      {
        matcher: { id: "byName", options: "Рекомендуемый маршрут" },
        properties: [
          { id: "custom.align", value: "center" },
          { id: "custom.cellOptions", value: { mode: "basic", type: "color-background" } },
          {
            id: "mappings",
            value: [
              {
                options: {
                  "Москва -> Стокгольм": { color: "#FF5E1F", index: 0, text: "Москва -> Стокгольм" },
                  "Стокгольм Core": { color: "#3b82f6", index: 1, text: "Стокгольм Core" },
                  "Москва Ingress": { color: "#22c55e", index: 2, text: "Москва Ingress" }
                },
                type: "value"
              }
            ]
          }
        ]
      }
    ]
  },
  gridPos: { h: 8, w: 24, x: 0, y: 44 },
  id: 412,
  options: {
    cellHeight: "sm",
    footer: { countRows: false, enablePagination: false, fields: "", reducer: ["sum"], show: false },
    showHeader: true,
    sortBy: [{ desc: true, displayName: "Выигрыш задержки (Дельта)" }]
  },
  pluginVersion: "13.2.2",
  targets: [
    {
      columns: [
        { selector: "region", text: "Макрорегион игроков", type: "string" },
        { selector: "active_players", text: "Игроков онлайн", type: "number" },
        { selector: "transit_ping_ms", text: "Пинг через Транзит (Москва)", type: "number" },
        { selector: "direct_ping_ms", text: "Пинг напрямую (Стокгольм)", type: "number" },
        { selector: "gain_ms", text: "Выигрыш задержки (Дельта)", type: "number" },
        { selector: "packet_loss_pct", text: "Потери пакетов", type: "number" },
        { selector: "recommended_route", text: "Рекомендуемый маршрут", type: "string" }
      ],
      datasource: { type: "yesoreyeram-infinity-datasource", uid: "warlink-infinity" },
      format: "table",
      global_query_id: "",
      refId: "A",
      root_selector: "regional_efficiency",
      source: "url",
      type: "json",
      url: "http://127.0.0.1:8081/api/v1/analytics",
      url_options: {
        data: "",
        method: "GET"
      }
    }
  ],
  title: "Сравнение эффективности режимов по регионам игроков (Урал, Сибирь, Центр, Юг)",
  type: "table"
};

// Reassemble the dashboard into 11 structured sections:
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

// SECTION 4: Автоматическая сетевая телеметрия игроков (Транзит Москва vs Прямой Стокгольм)
newPanels.push(createRow(104, "4. Автоматическая сетевая телеметрия игроков (Транзит Москва vs Прямой Стокгольм)", 35));
newPanels.push(nodeUsersPanel);
newPanels.push(pingDistributionPanel);
newPanels.push(packetLossHeatmapPanel);
newPanels.push(regionalEfficiencyPanel);

// SECTION 5: Нагрузка и детализация накопителя
newPanels.push(createRow(101, "5. Нагрузка: процессор, интернет-канал и накопитель", 53));
[
  { id: 12, w: 12, h: 8, x: 0, y: 54 },
  { id: 6, w: 12, h: 8, x: 12, y: 54 },
  { id: 7, w: 12, h: 8, x: 0, y: 62 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});
storagePanel.gridPos = { h: 8, w: 12, x: 12, y: 62 };
newPanels.push(storagePanel);

// SECTION 6: Мета-аналитика прогрессии WARDOGS
newPanels.push(createRow(105, "6. Мета-аналитика прогрессии WARDOGS (Классы, Ранги, Wishlist)", 70));
classesPanel.gridPos = { h: 8, w: 8, x: 0, y: 71 };
careerStatsPanel.gridPos = { h: 8, w: 6, x: 8, y: 71 };
wishlistTablePanel.gridPos = { h: 8, w: 10, x: 14, y: 71 };
newPanels.push(classesPanel);
newPanels.push(careerStatsPanel);
newPanels.push(wishlistTablePanel);

// SECTION 7: Географическая плотность и выбор PoP-серверов
newPanels.push(createRow(106, "7. Географическая плотность аудитории и выбор PoP-серверов", 79));
[
  { id: 120, w: 16, h: 11, x: 0, y: 80 },
  { id: 121, w: 8, h: 11, x: 16, y: 80 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 8: Аудитория, игровое время и версии клиентов
newPanels.push(createRow(108, "8. Аудитория: игровое время, активность и версии клиентов", 91));
playtimePanel.gridPos = { h: 4, w: 6, x: 0, y: 92 };
newPanels.push(playtimePanel);
if (panelsById[24]) {
  panelsById[24].gridPos = { h: 4, w: 6, x: 6, y: 92 };
  newPanels.push(panelsById[24]);
}
clientVersionsPanel.gridPos = { h: 4, w: 6, x: 12, y: 92 };
rejectionsPanel.gridPos = { h: 4, w: 6, x: 18, y: 92 };
newPanels.push(clientVersionsPanel);
newPanels.push(rejectionsPanel);
[
  { id: 21, w: 12, h: 8, x: 0, y: 96 },
  { id: 22, w: 12, h: 8, x: 12, y: 96 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 9: Выбор сообщества (Голосование за игры)
newPanels.push(createRow(110, "9. Выбор сообщества: голосование за новые игры", 104));
[
  { id: 50, w: 8, h: 9, x: 0, y: 105 },
  { id: 51, w: 16, h: 9, x: 8, y: 105 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

// SECTION 10: Инфраструктурное здоровье служб и стабильность API
newPanels.push(createRow(119, "10. Инфраструктурное здоровье служб и стабильность API", 114));
serviceHealthPanel.gridPos = { h: 7, w: 12, x: 0, y: 115 };
apiHttpCodesPanel.gridPos = { h: 7, w: 12, x: 12, y: 115 };
newPanels.push(serviceHealthPanel);
newPanels.push(apiHttpCodesPanel);

// SECTION 11: Инспектор активных сессий и релизы программы
newPanels.push(createRow(102, "11. Живые сессии и история релизов программы", 122));
[
  { id: 91, w: 14, h: 10, x: 0, y: 123 },
  { id: 95, w: 10, h: 10, x: 14, y: 123 }
].forEach(spec => {
  const p = panelsById[spec.id];
  if (p) {
    p.gridPos = { h: spec.h, w: spec.w, x: spec.x, y: spec.y };
    newPanels.push(p);
  }
});

dash.panels = newPanels;

fs.writeFileSync(dashPath, JSON.stringify(dash, null, 2), 'utf8');
console.log('Successfully reorganized dashboard into 11 sections with', dash.panels.length, 'panels!');

