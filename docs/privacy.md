# Конфиденциальность Marvia

Обновлено 25 сентября 2026 года. English version below.

Marvia — приложение для подключения к VPN-серверу по ключу или подписке,
которые вы получили от владельца сервера. Приложение не продаёт доступ и не
создаёт учётную запись в едином сервисе Marvia. Данные выбранной подписки
обрабатывает её владелец; его условия хранения нужно узнавать у него.

## На устройстве

Приложение хранит ключи и адреса подписок, список нод и их замеры, настройки
VPN и темы, выбор приложений для обхода туннеля, локальный журнал, расход и
историю сессий. Ключ VP1 содержит закрытый ключ и остаётся в хранилище
приложения: подтверждение доступа не требует отправлять его панели или ноде.
Для ключей других протоколов используются способы аутентификации этих
протоколов. Резервное копирование данных приложения средствами Android
выключено. «Сбросить всё» удаляет настройки, ключи, кэши подписок и учёт
трафика; история сессий и диагностические файлы остаются до удаления
приложения. Удаление приложения очищает его хранилище.

## Во время подключения

- **Панель подписки** получает запросы со списком нод, сроком и квотой. В
  запросе может быть токен подписки. Для ключей Marvia приложение по умолчанию
  передаёт панели результаты замеров нод; это можно выключить в дополнительных
  настройках VPN. При использовании чужой подписки её адрес и правила
  обработки определяет её владелец.
- **VPN-нода** принимает зашифрованное соединение и направляет ваш трафик.
  Ей доступен исходный сетевой адрес соединения; для учёта доступа и лимитов
  она обрабатывает идентификатор ключа и объём трафика. Владелец ноды может
  видеть трафик после выхода из туннеля в той мере, в какой его допускают
  протоколы посещаемых сервисов. Приложение не даёт гарантий за чужую ноду.
- **Определение страны выхода** иногда обращается к `ipapi.co`, когда у ноды
  нет страны в подписке. Запрос идёт **через ноду**, так что сервис видит её
  выходной IP-адрес, а не прямой адрес телефона. Ответ — двухбуквенный код
  страны, который сохраняется локально.
- **DNS и обход туннеля** зависят от выбранных вами настроек. Запросы имён
  получает выбранный DNS-резолвер; отмеченные приложения и маршруты идут
  напрямую и открывают адрес устройства соответствующим сервисам.

Приложение не содержит рекламных и аналитических SDK. Локальный журнал и
история сессий автоматически не отправляются автору приложения. Хранение
данных на панели или ноде и их удаление определяет владелец подписки, а не
кнопка удаления приложения с телефона.

## Разрешения Android

`VpnService` нужен для маршрутизации трафика через туннель. Интернет нужен
для получения подписки и соединения с нодой. Уведомление и служба на переднем
плане показывают состояние VPN при погашенном экране. Запуск после перезагрузки
работает, если вы его включили. Список установленных приложений используется
для настройки обхода туннеля и хранится на устройстве.

По вопросам о приложении можно открыть
[issue в проекте](https://github.com/jytt8u/marvia/issues). По вопросам о
данных на сервере обращайтесь к тому, кто выдал ключ доступа.

---

# Marvia privacy policy

Updated 25 September 2026.

Marvia connects to a VPN server using an access key or subscription supplied
by the server operator. The app does not sell access or create an account in
a central Marvia service. The operator of the subscription processes data on
their panel and nodes; ask them about their retention and deletion terms.

## On the device

The app stores access keys and subscription URLs, node lists and measurements,
VPN and appearance settings, the list of apps that bypass the VPN, local logs,
usage and session history. A VP1 private key remains in the app's private
storage: proving access does not require sending that key to the panel or
node. Other protocols authenticate according to their own specifications.
Android backup of app data is disabled. “Reset everything” removes settings,
keys, subscription caches and traffic totals; session history and diagnostic
files remain until the app is uninstalled. Uninstalling removes app storage.

## During a connection

- **The subscription panel** receives requests for nodes, expiry and quota.
  A request may carry a subscription token. For Marvia keys, the app sends
  node availability measurements by default; you can disable these reports
  in the advanced VPN settings. Third-party subscriptions are governed by
  their operators' terms.
- **The VPN node** accepts the encrypted connection and forwards traffic. It
  sees the connection's source IP address and processes a key identifier and
  traffic volume for access and quota enforcement. Beyond the VPN exit, the
  node operator may see traffic to the extent allowed by the destination's
  protocols. The app cannot make promises about third-party nodes.
- **Exit-country detection** sometimes queries `ipapi.co` when a subscription
  does not specify the node's country. This request goes **through the node**:
  the service sees its exit IP, not the phone's direct IP. The response is a
  two-letter country code stored locally.
- **DNS and VPN bypass** follow your settings. The selected DNS resolver
  receives DNS queries. Apps and routes excluded from the VPN connect directly
  and expose the device's address to their destinations.

The app has no advertising or analytics SDK. Local logs and session history
are not automatically sent to the app author. Deleting the app does not delete
data held by a subscription panel or node; its operator controls that data.

## Android permissions

`VpnService` routes device traffic into the tunnel. Internet access fetches
subscriptions and connects to nodes. The foreground service and notification
show VPN status while the screen is off. Starting after reboot is optional.
The installed-app list lets you choose apps that bypass the VPN and stays on
the device.

For questions about the app, open a
[project issue](https://github.com/jytt8u/marvia/issues). For data held on a
server, contact whoever supplied your access key.
