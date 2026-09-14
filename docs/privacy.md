# Политика конфиденциальности Marvia

*English version below.*

Marvia — это приложение для подключения к VPN-серверу того, кто дал вам
доступ. Ниже — что приложение хранит, что отправляет и кому. Здесь нет
ничего, чего нет в коде: он открыт, и этот документ ему следует.

## Кто отвечает за ваши данные

Приложение — инструмент. Сервер, к которому оно подключается, и панель,
выдавшая вам ключ, принадлежат **тому, кто дал вам доступ**. Он и есть
оператор ваших данных. Автор приложения не держит серверов, не получает от
приложения ничего и не может узнать, кто им пользуется.

## Что приложение хранит на устройстве

- **Ключ доступа** — ссылку `marvia://…`, которую вам прислали. В ней —
  ваш приватный ключ. Он остаётся в закрытом хранилище приложения и не
  покидает устройство: подключение доказывает владение ключом, не пересылая
  его.
- **Настройки**: язык, тема и её код, список приложений, идущих мимо
  туннеля, выбранная страна.
- **Журнал работы** — что происходило с подключением. Только на устройстве,
  виден в «Ещё → Логи», никуда не отправляется.

Удалили приложение — всё это удалилось вместе с ним.

## Что приложение отправляет

Приложение разговаривает ровно с двумя адресами, и оба принадлежат тому,
кто дал вам доступ:

1. **Панель** (адрес записан в вашем ключе). Приложение забирает у неё
   список серверов, срок и остаток трафика — по вашему токену подписки.
   Обратно уходят замеры серверов: какой отвечает и за сколько миллисекунд.
   Ни адрес вашего устройства, ни его модель, ни что-либо о вас в этих
   запросах нет; панель адреса не записывает.
2. **Сервер** (нода). Через него идёт ваш трафик. Сервер считает объём в
   байтах — чтобы работали квота и лимит скорости — и знает ваш публичный
   ключ. Если тот, кто дал доступ, ограничил число устройств, сервер держит
   в памяти адрес вашего устройства — не дольше часа и не записывая. Куда
   вы ходили, сервер не записывает: в нём нет такого журнала по устройству.

Сторонних серверов, аналитики, рекламных и отслеживающих библиотек в
приложении нет. Ни к Google, ни к кому-либо ещё оно не обращается.

## Разрешения

- **VPN** — чтобы направить трафик устройства через сервер. Приложение не
  читает и не изменяет содержимое трафика.
- **Интернет** — очевидно.
- **Уведомление и служба на переднем плане** — так Android держит VPN
  живым, когда экран выключен.
- **Запуск после перезагрузки** — только если вы включили автозапуск.
- **Список установленных приложений** — чтобы вы могли отметить, каким идти
  мимо туннеля (банк, такси). Список остаётся на устройстве.

## Ваш трафик

Пока туннель включён, трафик устройства идёт через сервер того, кто дал
вам доступ. Он зашифрован до сервера. Что происходит с трафиком после
сервера — вопрос к его владельцу, как и у любого VPN.

## Возраст

Приложение не собирает данных и потому не имеет возрастных ограничений по
этой части. Пользоваться им должен тот, кому выдан ключ.

## Изменения

Документ живёт в репозитории проекта вместе с кодом и меняется той же
правкой, что и поведение приложения. История изменений — в истории
репозитория.

---

# Marvia privacy policy

Marvia is an app for connecting to a VPN server run by whoever gave you
access. Below is what the app stores, what it sends and to whom. Nothing here
goes beyond the code: it is open, and this document follows it.

## Who is responsible for your data

The app is a tool. The server it connects to and the panel that issued your
key belong to **whoever gave you access**. They are the operator of your
data. The app's author runs no servers, receives nothing from the app and
cannot know who uses it.

## What the app keeps on the device

- **The access key** — the `marvia://…` link you were sent. It carries your
  private key. It stays in the app's private storage and never leaves the
  device: connecting proves possession of the key without sending it.
- **Settings**: language, theme and its code, the list of apps that bypass
  the tunnel, the chosen country.
- **A log** of what happened to the connection. On the device only, visible
  under "More → Logs", never sent anywhere.

Uninstall the app and all of this goes with it.

## What the app sends

The app talks to exactly two addresses, both owned by whoever gave you
access:

1. **The panel** (its address is in your key). The app fetches the server
   list, the expiry date and the remaining quota — by your subscription
   token. It sends back server measurements: which server answers and in
   how many milliseconds. Neither your device's address nor its model nor
   anything about you is in those requests; the panel does not record
   addresses.
2. **The server** (node). Your traffic goes through it. The server counts
   bytes — so that quotas and speed limits work — and knows your public
   key. If whoever gave you access limited the number of devices, the
   server keeps your device address in memory — for an hour at most and
   without writing it down. It does not record where you went: there is no
   such per-device log in it.

There are no third-party servers, no analytics, no advertising or tracking
libraries in the app. It contacts neither Google nor anyone else.

## Permissions

- **VPN** — to route the device's traffic through the server. The app does
  not read or alter the traffic's contents.
- **Internet** — obviously.
- **Notification and foreground service** — that is how Android keeps a VPN
  alive with the screen off.
- **Start after reboot** — only if you enabled autostart.
- **List of installed apps** — so you can mark which ones bypass the tunnel
  (banking, taxi). The list stays on the device.

## Your traffic

While the tunnel is on, the device's traffic goes through the server of
whoever gave you access. It is encrypted up to the server. What happens to
it beyond the server is a question for the server's owner, as with any VPN.

## Age

The app collects no data and therefore has no age restriction on that
account. It should be used by the person the key was issued to.

## Changes

This document lives in the project repository next to the code and changes
in the same commit as the app's behaviour. Its history is the repository's
history.
