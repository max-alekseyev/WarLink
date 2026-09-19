# Документация Aeza API v2 (Офлайн-копия)

Локальная автономная копия спецификации API личного кабинета хостинга **Aeza** (`https://my.aeza.net/api/v2`).

## Файлы в папке
* `openapi.json` — полная спецификация OpenAPI 3.0.0 (137 эндпоинтов, схемы данных DTO).
* `swagger-ui-init.js` — оригинальный бандл инициализации Swagger UI.
* `docs.html` — оригинальная веб-страница Swagger UI.

---

## Основные сведения

* **Базовый URL:** `https://my.aeza.net/api/v2`
* **Авторизация:** Заголовок `X-API-KEY: <ваш_api_ключ>` либо `Authorization: Bearer <токен>`.
* **Формат данных:** `application/json; charset=utf-8`.

---

## Ключевые эндпоинты биллинга и сервисов

### 1. Создание счета на пополнение (`POST /billing/invoices`)
* **Метод:** `POST /api/v2/billing/invoices`
* **Заголовки:** `X-API-KEY: <key>`, `Content-Type: application/json`
* **Тело запроса:**
  ```json
  {
    "method": "yookassa:sbp",
    "amount": 75
  }
  ```
  > **ВАЖНО по сумме (`amount`):**  
  > По спецификации OpenAPI `amount` передается в **minor currency units (центах)** базовой валюты аккаунта:  
  > Для европейских аккаунтов (EUR) `75` = **0.75 € (75 центов)**.  
  > При конвертации в рубли через ЮKassa/СБП по курсу ~130 руб./€ это составляет ровно **~98 ₽**.
* **Ответ (201 Created):**
  ```json
  {
    "id": 894123,
    "flowType": "default",
    "status": "created",
    "amount": 98,
    "payload": {
      "url": "https://yoomoney.ru/checkout/payments/v2/contract?orderId=..."
    },
    "createdAt": "2026-09-20T01:00:00.000Z"
  }
  ```
  * `payload.url` — прямая ссылка на форму оплаты СБП с QR-кодом для мобильного банка.
  * `amount` — фактическая сумма к списанию в рублях (например, 98 ₽).

### 2. Доступные способы оплаты (`GET /billing/payment-methods`)
* **Метод:** `GET /api/v2/billing/payment-methods`
* **Возвращает:** Список доступных шлюзов (ЮKassa, СБП, карты, крипта) с комиссиями (`commission`) и лимитами (`minAmount`, `maxAmount`).

### 3. Курсы обмена для способа оплаты (`GET /billing/payment-methods/{method}/exchange-rates`)
* **Метод:** `GET /api/v2/billing/payment-methods/{method}/exchange-rates`
* **Пример:** `GET /api/v2/billing/payment-methods/yookassa:sbp/exchange-rates`
* **Возвращает:** Актуальные курсы конвертации для выбранного платежного метода.

### 4. Список услуг и срок окончания аренды (`GET /services`)
* **Метод:** `GET /api/v2/services`
* **Возвращает:** Список серверов пользователя:
  ```json
  {
    "items": [
      {
        "id": 12345,
        "name": "WarLink Stockholm",
        "ip": "138.124.103.99",
        "typeSlug": "vps",
        "expiresAt": "2026-10-15T12:00:00Z",
        "status": "active",
        "price": 500
      }
    ]
  }
  ```
