# Архитектура мониторинга и телеметрии WarLink (Grafana + VictoriaMetrics)

## 1. Обзор архитектуры

Система мониторинга WarLink развернута на базе стека **VictoriaMetrics + Grafana 13.x** с интеграцией нативного источника данных **Infinity (REST Proxy)**:

- **TSDB (Сбор метрик):** VictoriaMetrics (`:8428`), опрос каждые 5 секунд (`scrape_interval: 5s`).
  - Таргет 1: `warlink-server:8080/metrics` (бизнес-метрики, сессии, HMAC-проверки, юнит-экономика, голосования).
  - Таргет 2: `node-exporter:9100/metrics` (CPU, SoftIRQ, сетевые интерфейсы, conntrack, сокет-буферы UDP).
- **Infinity Data Source (`warlink-infinity`):** безопасный Grafana-прокси к локальному REST API управления (`/api/v1/analytics`, `/api/v1/admin/settings`). Токен администратора инкапсулирован на уровне `secureJsonData` сервера и не передается в браузер пользователя.
- **Дашборд:** `docs/grafana_dashboard.json` (40 панелей, 4 строки/секции, поддержка шаблонизации `$node`, `$game`, `$interval`, `$__rate_interval`).
- **Unified Alerting:** `docs/grafana_alerts.yaml` (5 ключевых правил оповещений для предотвращения инцидентов).

---

## 2. Ключевые метрики сервиса

### Сетевой QoS и здоровье ядра Linux
- `node_cpu_seconds_total{mode="softirq"}`: загрузка софтверных прерываний сетевой карты (NET_RX/NET_TX).
- `node_netstat_Udp_RcvbufErrors`, `node_netstat_Udp_SndbufErrors`: дропы пакетов из-за переполнения сокет-буферов ядра Linux.
- `node_network_receive_packets_total`, `node_network_transmit_packets_total`: интенсивность пакетного потока (Packets Per Second).
- `node_nf_conntrack_entries` / `node_nf_conntrack_entries_limit`: утилизация таблицы отслеживания сетевых соединений.
- `warlink_session_terminations_total{reason="..."}`: причины завершения игровых сессий (`user_release`, `inactivity`, `ttl_expired`).

### Бизнес-метрики и юнит-экономика
- `warlink_sticky_factor`: показатель ежедневного удержания сообщества (`DAU / WAU * 100%`).
- `warlink_cpph`: себестоимость часа игры пользователя (Cost per Player-Hour в рублях).
- `warlink_checkout_conversion`: конверсия из выставленных счетов на пожертвования в успешно оплаченные.
- `warlink_server_runway_days`: финансовый горизонт автономности сервера с учетом баланса провайдера.
- `warlink_game_votes{title="..."}`: статистика голосования сообщества за добавление новых игр.

---

## 3. Правила Unified Alerting

Файл конфигурации: `/etc/grafana/provisioning/alerting/rules.yaml` (репозиторий: `docs/grafana_alerts.yaml`).

| Правило | Условие (PromQL) | Длительность (`for`) | Важность | Описание |
| :--- | :--- | :--- | :--- | :--- |
| **warlink_gateway_down** | `up{job="warlink-server"} == 0` | 1m | `critical` | Сервис API / Hysteria 2 не отвечает на запросы |
| **warlink_capacity_saturation** | `increase(warlink_session_rejections_total{reason="capacity"}[5m]) > 0` | 0s | `warning` | Исчерпан лимит слотов (все 100 игровых слотов заняты) |
| **warlink_hmac_replay_spike** | `rate(warlink_session_rejections_total{reason="bad_signature"}[2m]) > 5` | 1m | `warning` | Всплеск невалидных подписей HMAC (атака/рассинхрон) |
| **warlink_runway_depleted** | `warlink_server_runway_days < 7` | 1h | `warning` | До окончания оплаченного периода сервера осталось менее 7 дней |
| **warlink_cpu_overload** | `CPU > 90%` | 5m | `high` | Непрерывная высокая утилизация процессора хоста |

---

## 4. Настройка Contact Point (Уведомления в Telegram)

Для отправки алертов в канал или чат администраторов:

1. В веб-интерфейсе Grafana перейти в **Alerting -> Contact points -> Add contact point**.
2. Имя: `telegram-sre`.
3. Тип: `Telegram`.
4. Параметры:
   - **BOT API Token:** токен Telegram-бота (от `@BotFather`).
   - **Chat ID:** идентификатор чата или группы администраторов.
5. Нажать **Test** и сохранить.
6. В разделе **Notification policies** установить `telegram-sre` в качестве получателя по умолчанию (Default policy) или создать маршрут для меток `service = warlink`.
