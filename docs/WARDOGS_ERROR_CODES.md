# Канонический справочник ошибок WARDOGS (Bulkhead Interactive)

Справочник составлен на основе прямого реверс-инжиниринга бинарных файлов игры (`WardogsLauncher-Shipping.exe`, `WardogsClient-Win64-Shipping.exe`, `service.exe`), анализа секций памяти `.text`, `.rdata`, сетевых протоколов Pragma Engine и службы античита Elytra.

---

## 1. Карта источников ошибок в бинарном коде игры

Все коды ошибок лаунчера жестко зашиты в исполняемом файле `WardogsLauncher-Shipping.exe` (Steam AppID 1867240).

### Архитектура диспетчера ошибок в лаунчере:
* **Файл**: `C:\Program Files (x86)\Steam\steamapps\common\Wardogs\WardogsLauncher-Shipping.exe` (размер 1 333 144 байт, сборка Shipping, Release 1.2).
* **Секция таблицы сопоставления**: `.rdata` (Virtual Address `0x140011000`, файловое смещение `0x0000F800`).
* **Таблица дескрипторов ошибок**: Расположена по базовому смещению `0x00011550` (VA `0x140012D50` – `0x140012EA8`). Состоит из 22 последовательных 16-байтовых структур `ErrorDescriptor`:
  ```c
  struct ErrorDescriptor {
      const char* failure_operation; // Указатель на имя этапа (8 байт)
      const char* error_code;        // Указатель на строку "WD-L0xx" (8 байт)
  };
  ```
* **Пул строк кодов ошибок (`WD-L***`)**: Расположен в `.rdata` по смещениям `0x000116B0` – `0x00011758` (VA `0x140012EB0` – `0x140012F58`).
* **Пул строк операций (`failure_operation`)**: Расположен в `.rdata` по смещениям `0x0000FFC8` – `0x000113C0` (VA `0x1400117C8` – `0x140012BC0`).
* **Функция форматирования телеметрии и краш-репорта**: Функция `FormatCrashReport` по виртуальному адресу `0x140008B00` (файловое смещение `0x00007F00` в секции `.text`).
* **Эндпоинт отправки телеметрии**: `https://042f03b1507e3cb122e6fc988ca5d51a@faa.bulkhead.net/6507359` (строка в `.rdata` по смещению `0x00011400`).

---

## 2. Полная таблица ошибок лаунчера с точными адресами в коде

| Код ошибки | Этап (`failure_operation`) | Источник в бинарнике (Таблица / Код VA / Операция VA) | Описание и техническая причина сбоя | Метод решения |
| :--- | :--- | :--- | :--- | :--- |
| **WD-L000** | `generic-fallback` | `0x14000890F` (RIP-rel ref в `.text`) | Общий резервный обработчик неперехваченных исключений среды | Перезапуск игры, проверка целостности файлов Steam |
| **WD-L001** | `window` | Смещение `0x11550` \| Код: `0x140012EB0` \| Этап: `0x1400117C8` | Сбой WinAPI `CreateWindowEx` или инициализации оверлея D3D11/12 | Отключить сторонние оверлеи (Discord, RivaTuner, GeForce Experience) |
| **WD-L002** | `thread` | Смещение `0x11560` \| Код: `0x140012EB8` \| Этап: `0x1400117E0` | Ошибка WinAPI `CreateThread`, исчерпание пула потоков Windows | Перезагрузить Windows, добавить игру в доверенные программы антивируса |
| **WD-L003** | `http-init` | Смещение `0x11570` \| Код: `0x140012EC0` \| Этап: `0x1400126D8` | Сбой инициализации сессии WinHTTP (`WinHttpOpen`) | Сброс сетевого стека Windows (`netsh winsock reset`, перезагрузка) |
| **WD-L004** | `config-fetch` | Смещение `0x11580` \| Код: `0x140012EC8` \| Этап: `0x1400127A0` | **Таймаут/сброс HTTPS-запроса `/v1/env/.../cfg?s=...` к `faa.bulkhead.net` или `elytra.ac`**. В РФ блокируется или сбрасывается ТСПУ по сигнатуре SNI/IP | **Использовать игровой туннель WarLink**, направляющий процесс лаунчера через игровой шлюз |
| **WD-L005** | `config-parse` | Смещение `0x11590` \| Код: `0x140012ED0` \| Этап: `0x1400127C8` | Сбой парсинга JSON манифеста (провайдер перехватил трафик и отдал HTML страницу заглушки/Captive Portal) | Проверить авторизацию в сети провайдера, использовать зашифрованный туннель |
| **WD-L006** | `service-unavailable` | Смещение `0x115A0` \| Код: `0x140012ED8` \| Этап: `0x1400126A0` | Служба Windows `Elytra` (`service.exe`) не установлена, остановлена или отключена | Запустить `Elytra\Elytra-Setup.exe` от имени администратора, выполнить установку |
| **WD-L007** | `pipe-connect` | Смещение `0x115B0` \| Код: `0x140012EE0` \| Этап: `0x1400126B8` | Не удалось подключиться к именованному каналу `\\.\pipe\elytra...` (`ERROR_PIPE_BUSY` / `ERROR_ACCESS_DENIED`) | Перезапустить службу `Elytra` (`sc stop Elytra && sc start Elytra`) |
| **WD-L008** | `module-query` | Смещение `0x115C0` \| Код: `0x140012EE8` \| Этап: `0x1400128D0` | Ошибка вызова IPC-команды `IsModuleInstalled` к службе античита | Проверить права доступа на чтение/запись каталога `C:\Program Files\Elytra\` |
| **WD-L009** | `module-download` | Смещение `0x115D0` \| Код: `0x140012EF0` \| Этап: `0x140012680` | Не удалось загрузить `.cab` архив модуля защиты с CDN Fastly (`elytra.ac`) | Проверить интернет-соединение, направить трафик через шлюз WarLink |
| **WD-L010** | `module-install` | Смещение `0x115E0` \| Код: `0x140012EF8` \| Этап: `0x140012690` | Ошибка вызова `AddInstallModule` (недостаточно места на диске, блокировка записи) | Освободить место на диске C:, проверить права администратора |
| **WD-L011** | `module-untrusted` | Смещение `0x115F0` \| Код: `0x140012F00` \| Этап: `0x140012650` | Корневой сертификат разработчика не найден в системном хранилище доверенных корней | Обновить корневые сертификаты Windows через Windows Update |
| **WD-L012** | `module-signature` | Смещение `0x11600` \| Код: `0x140012F08` \| Этап: `0x140012668` | Ошибка WinVerifyTrust (0x800B0109/0x800B010E) либо недоступность серверов CRL/OCSP (порт 80) | Проверить целостность файлов Steam, открыть доступ к CRL-серверам (порт 80 direct) |
| **WD-L013** | `session-create` | Смещение `0x11610` \| Код: `0x140012F10` \| Этап: `0x140012938` | Ошибка в IPC-методе `GameSessionCreate` (конфликт с неподписанными драйверами или отладчиками) | Отключить тестовый режим Windows (`bcdedit /set testsigning off`), закрыть программы отладки |
| **WD-L014** | `session-prime` | Смещение `0x11620` \| Код: `0x140012F18` \| Этап: `0x140012B30` | Античит не смог зафиксировать начальный снимок памяти игры (`GameSession_Prime`) | Перезагрузить компьютер, запустить игру заново |
| **WD-L015** | `session-configure` | Смещение `0x11630` \| Код: `0x140012F20` \| Этап: `0x140012958` | Не удалось применить правила изоляции и список модулей (`GameSession_Configure`) | Перезапустить службу `Elytra` |
| **WD-L016** | `module-unresolved` | Смещение `0x11640` \| Код: `0x140012F28` \| Этап: `0x140012A90` | В сессии обнаружен поврежденный модуль защиты, попытка автоматического ремонта провалилась | Запустить `Elytra-Setup.exe` и нажать Repair (Восстановление) |
| **WD-L017** | `exe-path` | Смещение `0x11650` \| Код: `0x140012F30` \| Этап: `0x140012AA8` | Лаунчер не смог разрешить путь к `WardogsClient-Win64-Shipping.exe` (спецсимволы, повреждение пути) | Установить Steam и игру в путь, содержащий только латиницу |
| **WD-L018** | `exe-missing` | Смещение `0x11660` \| Код: `0x140012F38` \| Этап: `0x140012B08` | Файл `Wardogs\Binaries\Win64\WardogsClient-Win64-Shipping.exe` отсутствует на диске | Проверить карантин антивируса, запустить проверку целостности файлов Steam |
| **WD-L019** | `environment` | Смещение `0x11670` \| Код: `0x140012F40` \| Этап: `0x140012B68` | Ошибка вызова `SetEnvironmentVariable` при пробросе параметров запуска Steam | Перезапустить клиент Steam от имени администратора |
| **WD-L020** | `process-start` | Смещение `0x11680` \| Код: `0x140012F48` \| Этап: `0x140012B98` | Ошибка вызова WinAPI `CreateProcess` (блокировка антивирусом, SmartScreen, отсутствие прав) | Добавить папку игры в исключения антивируса Windows Defender |
| **WD-L021** | `start-notify` | Смещение `0x11690` \| Код: `0x140012F50` \| Этап: `0x140012BC0` | Лаунчер не смог передать сигнал `GameSession_StartNotify` службе античита в заданный таймаут | Снизить нагрузку на процессор при старте игры |
| **WD-L022** | `out-of-memory` | Смещение `0x116A0` \| Код: `0x140012F58` \| Этап: `0x140012948` | Недостаточно оперативной памяти для размещения структур лаунчера | Увеличить файл подкачки Windows, закрыть ресурсоемкие приложения |

---

## 3. Источники ошибок в службе античита Elytra (`service.exe`, `control.exe`)

* **Архитектура и исполняемые файлы**:
  * Служба: `C:\Program Files\Elytra\service.exe` (написана на Rust, Tokio runtime, Session 0, `NT AUTHORITY\SYSTEM`).
  * Утилита управления: `C:\Program Files\Elytra\control.exe` (клиент управления службой).
  * Установщик: `C:\Program Files (x86)\Steam\steamapps\common\Wardogs\Elytra\Elytra-Setup.exe`.
  * Хранилище модулей: `C:\Program Files\Elytra\Content\`.
* **Модули защиты**: Доставляются в подписанных Authenticode `.cab`-архивах с CDN `elytra.ac` (издатель: Vaiiya Corporate Limited, отпечаток SHA-1: `AB9527FA0A67115A28799C0D7551BBE42190A0EE`). Модули реализуют интерфейс `IElytraModule`.
* **IPC-интерфейс**: Именованный канал `\\.\pipe\Elytra.ServiceServer` по протоколу JSON-RPC 2.0 с заголовком `Content-Length`:
  * `GameSessionCreate`: создание защищенной сессии. Возвращает UUID сессии. Ошибка `-32603`: "A game session is already active".
  * `GameSession_Configure`: передача модулей безопасности (`modules`). Ошибки: "Failed to resolve module", "Missing dependency", "Circular dependency detected".
  * `GameSession_Prime`: фиксация контрольного состояния модулей под `WardogsClient-Win64-Shipping.exe`. Ошибки: "Invoking IElytraModule::prime failed".
  * `GameSession_StartNotify`: подтверждение запуска процесса игры (`pid`, `creation_time`). Ошибки: "Process creation time mismatch (PID reused)", "Failed to call StartNotify on module".
  * `InstallModule` / `IsModuleInstalled`: проверка наличия и установка CAB-модулей безопасности.
* **Поведение античита во время матча (Ingame Kicks)**:
  * В `service.exe` полностью отсутствуют импорты графического интерфейса (`user32.dll` / `MessageBoxW`).
  * Мониторинг процесса клиента ведется через системные хэндлы с правами синхронизации (`crates/bin/service/src/process_tracker.rs`).
  * При обнаружении нарушений (инжекция DLL, хуки ядра, WinDivert):
    1. **Принудительное завершение**: вызов `TerminateProcess` или перехват исключения -> краш на рабочий стол или окно `CrashReportClient.exe`.
    2. **Сетевой кик**: сбой криптографической аттестации сессии на сервере -> разрыв сессии сервером с кодом Unreal Engine "Connection to the host has been lost".
* **Внутренние Enum-типы ошибок Rust в service.exe**:
  * `ServiceControllerError`: `OpenSCManagerFailed`, `ServiceNotFound`, `OpenServiceFailed`, `QueryServiceStatusFailed`, `StartServiceFailed`, `ServiceDisconnect`.
  * `wincrypt::cert::CertError`: `CreateContextFailed`, `GetPropertyFailed`, `BufferTooSmall`, `InvalidContext`, `InvalidPem`.
  * `wintrust::WinTrustError`: `VerificationFailed`, `StateNotAvailable`, `NoProviderData`, `NoSigner`, `NoCertificate`.
  * `cab::extract::CabError`: `CreateOutputDir`, `OpenInputFile`, `FdiCreate`, `FdiCopy`, `InvalidEntryPath`, `CreateOutputFile`.
* **Коды JSON-RPC в Elytra**:
  * `-32700`: `ParseError` (Некорректный JSON)
  * `-32600`: `InvalidRequest` (Недопустимый запрос)
  * `-32601`: `MethodNotFound` (Метод не зарегистрирован)
  * `-32602`: `InvalidParams` (Недопустимые параметры вызова)
  * `-32603`: `InternalError` (Внутренняя ошибка сервиса)
* **Системные Win32 и NTSTATUS коды**:
  * `0x00000005 (ERROR_ACCESS_DENIED)`: Отсутствие административных прав при обращении к пайпу/службе.
  * `0x000000E7 (ERROR_PIPE_BUSY)`: Экземпляры именованного канала заняты.
  * `0x00000424 (ERROR_SERVICE_DOES_NOT_EXIST)`: Служба Elytra не зарегистрирована в реестре.
  * `0x0000041D (ERROR_SERVICE_NEVER_STARTED)`: Драйвер ядра не смог стартовать из-за блокировки HVCI (Изоляция ядра) или Test Signing.
  * `0x800B0109 (CERT_E_UNTRUSTEDROOT)`: Недоверенная цепочка цифровой подписи драйвера.
  * `0x800B010C (CERT_E_REVOKED)`: Сертификат отозван.

---

## 4. Источники внутриигровых сетевых ошибок Pragma Engine

* **Исполняемый файл**: `Wardogs\Binaries\Win64\WardogsClient-Win64-Shipping.exe` (защищен Denuvo Anti-Tamper, распаковщик `runtime.dll` / `coreinit.dll`).
* **Сетевой движок**: Pragma Engine C++ SDK (встроен в Unreal Engine 5).
* **Сетевые эндпоинты в бинарнике**:
  * `social.live.wardogs.bulkhead.pragmaengine.com` (WebSocket WSS / Protobuf RPC)
  * `game.live.wardogs.bulkhead.pragmaengine.com` (Матчмейкинг и координация сессий)
  * `faa.bulkhead.net` (Federated Authentication & Fleet Allocation)
* **Протокольные структуры ошибок (`pragma.protocol.ErrorResponse` / `FPragmaError`)**:
  * **Сессия и авторизация**:
    * `PRAGMA_ERR_UNAUTHENTICATED` (401) — истек токен сессии Steam.
    * `PRAGMA_ERR_PERMISSION_DENIED` (403) — аккаунт заблокирован или ветка недоступна.
    * `PRAGMA_ERR_GATEWAY_NOT_CONNECTED` — потеряно постоянное соединение со шлюзом.
    * `PRAGMA_ERR_HEARTBEAT_TIMEOUT` — таймаут пакета пинга (>15с).
    * `PRAGMA_ERR_RATE_LIMITED` (429) — превышен лимит частоты запросов.
  * **Группа и социальные сервисы (`SocialError`)**:
    * `SOCIAL_PARTY_FULL` — группа заполнена.
    * `SOCIAL_PARTY_NOT_FOUND` — группа расформирована.
    * `SOCIAL_NOT_PARTY_LEADER` — действие доступно только лидеру.
    * `SOCIAL_INVITE_EXPIRED` — срок действия приглашения истек.
  * **Матчмейкинг и игровой цикл (`GameLoopError`)**:
    * `GAMELOOP_MATCHMAKING_TIMEOUT` — таймаут ожидания матча.
    * `GAMELOOP_FLEET_UNAVAILABLE` — нет свободных серверов в регионе.
    * `GAMELOOP_PLAYER_SESSION_ALLOCATION_FAILED` — сбой бронирования слота игрока на сервере.

---

## 5. Сетевые ошибки Unreal Engine 5 и серверов AWS GameLift

* **Unreal Engine 5 NetDriver & Travel Failure**:
  * `ENetworkFailure::ConnectionLost` — "Lost connection to the host."
  * `ENetworkFailure::ConnectionTimeout` — "Connection to the game server timed out."
  * `ENetworkFailure::OutdatedClient` — "Your client is out of date. Please update via Steam."
  * `ETravelFailure::ClientTravelSocketFailure` — сбой создания сокета для перехода на сервер матча.
  * `OnlineBeacon_ReservationDenied` — "Server is full. Unable to join match."
* **Выделенные серверы AWS GameLift (UDP `4000:4500`)**:
  * `GameSessionFullException` — матч переполнен.
  * `FleetCapacityExceededException` — исчерпана квота вычислительных ресурсов флота.
  * `PlayerSessionNotFoundException` — игрок не зарегистрирован в сессии матча.
* **Голосовой движок Vivox (`vivoxsdk.dll`)**:
  * `VX_MEDIA_ROSTER_UPDATE_NETWORK_TIMEOUT` (20650) — разрыв RTP-потока передачи голоса.
  * `VxErrorConnectionTimeout` (20483) — сбой подключения к SIP-серверу координации.
  * `VxErrorTokenExpired` (20500) — истек токен авторизации голосовой сессии.

---

## 6. Метаданные Unreal Engine 5 Zen Store (`global.ucas`) и UI-классы ошибок

Вся метаинформация движка Unreal Engine 5, сериализованные FNames (64 527 имен), схема Pragma Platform и структуры интерфейса ошибок вынесены в контейнер `Content\Paks\global.ucas` (4 575 120 байт) и `global.utoc`:

* **Таблица FNames (Name Batch Buffer)**:
  * Заголовок: смещение `0x00000000` (`NumNames = 64 527`, `StringBufferSize = 1 716 738` байт).
  * Таблица 16-битных длин: `0x0007E088` (длина 129 054 байт).
  * Буфер строк: `0x0009D8CE` – `0x00240AA8`.
* **Ключевые смещения имен классов и enum-ошибок в global.ucas**:
  * `EWDOnlineErrorType`: смещение `0x002196C5` (индекс имени: 58208 / `0xE360`) — корневой enum ошибок WARDOGS.
  * `WDOnlineErrorPopupConfig`: индекс имени 60234 — конфигурация модального диалога ошибки.
  * `EPragmaConnectionError`: смещение `0x001B64F1`.
  * `EPragma_Matchmaking_MatchmakingFailureReason`: смещение `0x001B60BF`.
  * `EPragma_Matchmaking_LeftMatchmakingReason`: смещение `0x001B6096`.
  * `EPragma_CustomApplicationErrors_PlacementFailureReason`: смещение `0x001B5E60`.
  * `EBanReason`: смещение `0x0019F03B`.
  * `ENetworkFailure`: смещение `0x001909B6`.
  * `ETravelFailure`: смещение `0x001048CF`.
  * `EFirstLookAuthError`: смещение `0x00128EAD`.
  * `Pragma_AntiCheat_CheaterDetectedV1Notification`: смещение `0x001B806C`.
  * Таблица экспортов Zen ScriptObjects: смещение `0x00240AA8`.
* **UI-виджеты и подсистемы отображения ошибок клиенту**:
  * `WDOnlineErrorPopupConfig` / `EWDPopupState` / `EWDPopupOption` — отрисовка всплывающего окна ошибки.
  * `WDUIServerInfoPopupWidget` / `WDUIServerInfoPopupViewModel` — статус сервера матча.
  * `WDNetStatusSnapshot` / `WDNetStatusSettings` / `WDNetStatusSubsystem` — монитор пинга, джиттера и потерь пакетов.
* **Структура краш-репорта лаунчера (`ElytraLauncher.last-error.json`)**:
  ```json
  {
    "time": "ISO-8601",
    "launcher": "wardogs.launcher",
    "launcher_version": "1.2",
    "summary": "Текст ошибки",
    "error_code": "WD-L001 .. WD-L022",
    "failure_kind": "service-unavailable | pipe-connect | ...",
    "failure_operation": "StartNotify | ConnectServicePipe | ...",
    "native_error": "0x%08lx",
    "launch-id": "12-hex-trace-id"
  }
  ```

---

## 7. Статус внутриигровых кодов: существуют ли WD-C***, WD-G***, WD-M***

Прямой побайтовый и строковый анализ `global.ucas`, `WardogsClient-Win64-Shipping.exe`, `runtime.dll` и `service.exe` однозначно доказал:
1. **Кодов вида `WD-C***`, `WD-G***`, `WD-M***`, `WD-N***`, `WD-S***` в игре не существует.**
2. Префикс **`L`** в `WD-L***` строго и исключительно означает **`Launcher`**.
3. Внутри запущенного игрового клиента игра WARDOGS использует не короткие буквенно-цифровые шифры, а строгую типизацию классов Pragma Engine (`Pragma ApplicationError`) и перечисления Unreal Engine 5 (`EWDOnlineErrorType`, `ENetworkFailure`, `ETravelFailure`).
