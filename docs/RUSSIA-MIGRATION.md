# Перенос notes-bot: server → hostkey-russia

Подготовка 31 октября 2026. До завершения runtime-проверок этот документ
описывает порядок действий, а не подтверждённое переключение.

## Размещение

На Russia переносятся все пять компонентов, обе PostgreSQL, весь локальный
`/home/maxim/Yandex.Disk` (включая `.sync` и Git vault), конфигурация клиента
и каталог этого бота в общем локальном Telegram Bot API. В архиве исходного
vault сохраняются root-owned файлы; рабочая копия на Russia принадлежит maxim.
Core и Search запускаются с UID/GID владельца vault. На источнике найдены
29 файлов root:root 0600, недоступных Yandex Disk и поиску; новые файлы должны
создаваться под UID синхронизации. Содержимое и Git при смене владельца не меняются.

Обычный запуск выбирает основной Compose и `compose/compose.russia.yml` через
приватный `COMPOSE_FILE`. Overlay сохраняет имена project/services/volumes,
но использует явную свободную подсеть: default Docker pools Russia исчерпаны.
`NOTES_VAULT_UID/GID`, `NOTES_SUBNET` задаются по фактическому хосту.
Образы БД закрепляются digests исходных контейнеров в `NOTES_POSTGRES_IMAGE`
и `NOTES_SEARCH_POSTGRES_IMAGE` как метаданные переноса. Приложения — исходные
проверенные GHCR digests либо новая прошедшая CI сборка; на VM не собирать.
Make передаёт выбранные overlays в `image-deploy`, чтобы CD их сохранял.

## Временные зависимости от server

Перенос notes-bot пока не разрешает выключить server:

| Зависимость | Доступ с Russia |
| --- | --- |
| Kafka, прежний topic и consumer offsets | `kafka:9092`, hostname mapped на `100.64.0.1` |
| Redis, прежние пользовательские состояния | `100.64.0.1:16379` |
| location и его БД | `100.64.0.1:18084` |
| Jaeger | `100.64.0.1:4317` |

Новые TCP forwards — только tailnet, через существующий tailscaled.
`scripts/legacy_forward.py` на server находит единственный контейнер по Compose
labels и его адрес в нужной сети, не полагаясь на отображаемые имена.
`notes-legacy-forward.timer` обновляет адреса раз в минуту после пересоздания
контейнеров; другие Serve listeners не меняются. Root-owned скрипт установлен
в `/usr/local/lib/notes-backup`, units — в `/etc/systemd/system/`.
Отключить этот timer и только три listener после переноса общих зависимостей.
До этого не удалять Kafka/Redis/location source volumes.

Transcriber уже на Russia (`100.64.0.5:50053`), новый Bot API — местный.
Перенос не запускает новые inference workers или модели на WSL.
LLM-конфигурация сохраняется; доступность прежнего Ollama проверять отдельно.

## Безопасный порядок

1. Снять и проверить внешний backup на ASUS; подготовить destination checkout,
   конфигурацию, образы и внешние сети. Первично скопировать файлы через rsync,
   используя временный root-read-only `rrsync` key, ограниченный Russia и
   каталогом `/home/maxim/Yandex.Disk`. На Russia sync пока не запускать.
2. Заблокировать CD исходного хоста. Остановить Telegram, web, scheduler, core,
   search; остановить оба старых yandex-disk и убрать только его автозапуск.
   Сохранить cron и исходную конфигурацию в приватном backup. Никакого git reset
   или удаления незакоммиченных изменений vault.
3. Снять финальные pg_dump и fingerprints всех таблиц, выполнить финальный rsync
   и сравнить SHA-256 каждого локального файла и Git. Сохранить финальные файлы,
   `.sync`, config и дампы вне исходной VM. Приложения на destination ещё выключены.
4. Восстановить БД в новые volumes той же версии, сравнить все таблицы.
   Закрыть старую Bot API instance через deleteWebhook(drop_pending_updates=false)
   и close, перенести **только** каталог notes-bot с прежними правами в общий
   destination API volume. Других ботов и их данные не менять.
5. Запустить core/search/web, проверить чтение и запись синтетического файла,
   БД, Transcriber и общие зависимости. Затем Telegram и scheduler, проверить
   регистрацию webhook и отсутствие второго экземпляра на server.
6. Запустить единственный `yandex-disk.service` на Russia с прежними auth,
   iid и exclusions. Клиент 0.1.6.1080 перенесён из того же официального deb.
   Сохранённое дерево перед первым подключением не удалять: синхронизация
   может применить изменения из облака. Проверить доступность всех локальных файлов.
   Foreground-режим старого клиента не читает config.cfg: `scripts/yandex_disk.py`
   явно переносит его настройки в аргументы, включая exclusions и auth path.
   Launcher устанавливается root-owned в `/usr/local/lib/notes-backup/`.
7. Направить GitHub VM_* secrets/CD на Russia, проверить новый workflow и
   фактические digests/runtime. На source поставить restart=no и защиту retired.
8. Переключить ASUS backup source на Russia, вручную выполнить backup/restore,
   включить оба timers. Отозвать временный rsync key после финальной сверки.

Локальный web остаётся на `127.0.0.1:8090`; публичный сайт этим переносом
не открывается. Для административного доступа можно использовать SSH tunnel.

## Откат

До новых записей остановить destination writers/sync/API instance и вернуть
исходную конфигурацию/контейнеры. После записей или облачной синхронизации
старые snapshots устарели: сначала остановить writers, перенести **актуальные**
файлы, обе БД, состояние sync и Bot API обратно с проверкой fingerprints.
Не запускать параллельно два writers, sync-клиента для этой серверной копии
или Telegram instances. Telegram-доставка необратима: сверить offsets и voice
statuses перед включением, не сбрасывать Kafka group.

Резервное копирование и ограничения — [BACKUPS.md](BACKUPS.md).
Порядок переноса локального Bot API следует
[официальной инструкции](https://github.com/tdlib/telegram-bot-api#moving).
