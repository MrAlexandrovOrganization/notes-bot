# Синхронизация заметок через Яндекс Диск

Russia синхронизирует `/home/maxim/Yandex.Disk/notes` с `notes` в Яндекс Диске
через `rclone bisync`. На Mac остаётся штатное приложение Яндекс Диска.
Состояние `yandex-disk status` на Russia больше не относится к активному клиенту.

15 ноября 2026 проверены: официальный Linux-клиент 0.1.6.1081 устраняет ошибки
доверия сертификатам версии 0.1.6.1080, но отправка небольшой дельты существующего
файла воспроизводимо получает HTTP 500. Общий OAuth-клиент rclone получает HTTP
410. Используется личное OAuth-приложение с `cloud_api:disk.read`,
`cloud_api:disk.write`, `cloud_api:disk.info`; адрес API и маршруты не подменяются,
проверка TLS включена. Версия rclone задаётся в `makefiles/versions.mk`.

## Установка и запуск

Это административная установка systemd, отдельно от CD контейнеров notes-bot.
Prerequisites: Linux/systemd, Python 3.12+, rclone указанной версии, пользователь
`maxim`, рабочее соединение HTTPS с Яндексом. Официальный бинарник из релиза
`rclone/rclone` проверяется по SHA-256 из метаданных релиза GitHub до установки.

Установленные root-owned файлы:

- `/usr/local/lib/notes-sync/rclone` — проверенный бинарник;
- `/usr/local/lib/notes-sync/notes_sync.py` — `scripts/notes_sync.py`;
- `/usr/local/lib/notes-sync/filters.txt` — `ops/notes-sync/filters.txt`;
- `/etc/systemd/system/notes-bisync.{service,timer}` — units из `ops/notes-sync/`.

Конфигурация `/home/maxim/.config/rclone/notes.conf` принадлежит `maxim`, mode
0600. Remote `notes-yandex` имеет тип `yandex`, `upload_wait = 2s`, личные
ClientID/secret и OAuth JSON с настоящим refresh token. Секреты передаются только
через защищённые файлы, не через аргументы, Git, сообщения или логи.

Состояние: `~/.local/state/notes-bisync`, резервные версии:
`~/.local/state/notes-bisync-backups` и `notes-sync-backups/russia` в облаке,
вне папки заметок. Каталоги на Russia создаются заранее, владелец `maxim`, 0700.
Рабочая копия фильтров и создаваемый bisync файл `.md5` находятся в каталоге
состояния; канонический файл в `/usr/local/lib` остаётся доступным только для чтения.

Перед первым включением сохранить обе стороны, сверить файлы по хэшам и
разобрать расхождения. Затем остановить `yandex-disk.service`, создать одинаковый
файл `.notes-sync-access` в локальном и облачном vault. Только после этого:

```sh
python3 /usr/local/lib/notes-sync/notes_sync.py bootstrap-preview
python3 /usr/local/lib/notes-sync/notes_sync.py bootstrap
sudo systemctl disable yandex-disk.service
sudo systemctl enable --now notes-bisync.timer
```

Команды Python выполнять как `maxim`, не root. Обе bootstrap-команды сначала
выполняют полный `rclone check` и отказываются выбирать сторону при расхождениях.
Обычный запуск никогда не использует `--resync`. Bootstrap не запускать
автоматически при ошибках: сначала проверить состояние обеих сторон и архивы.

## Гарантии и ограничения

Таймер запускает следующий проход через минуту после завершения предыдущего.
Это периодический обмен, не мгновенный файловый watcher. Блокировка rclone
исключает одновременные проходы. Unit конфликтует с прежним клиентом, ограничен
по памяти и времени; остановка передаёт SIGINT для graceful shutdown.

Сравнение использует размер и MD5, поддерживаемый Яндекс Диском. Поддерживается
восстановление после прерванного прохода без слепой переинициализации состояния.
Отсутствие контрольного файла блокирует обмен. Массовое удаление свыше 1% файлов
останавливает проход для разбора. Изменённые и удалённые версии сохраняются на
обеих сторонах с уникальным суффиксом; при конфликте остаются обе версии.
Автоматической очистки этих архивов нет: контролировать свободное место.

Из обмена исключены `.git`, `.DS_Store` и `*.notes-bot.lock`; существующие
исключённые файлы не удаляются. Git-история остаётся на хостах и в полных внешних
копиях ASUS. Остальные файлы, включая вложения и прогресс обучения `.notes-bot`,
синхронизируются. Контрольный файл `.notes-sync-access` удалять нельзя.

```sh
systemctl status notes-bisync.timer notes-bisync.service
sudo journalctl -u notes-bisync.service --since today
python3 /usr/local/lib/notes-sync/notes_sync.py check
sudo systemctl stop notes-bisync.timer notes-bisync.service
```

Возврат к прежнему клиенту требует сначала остановить таймер и текущий проход;
не включать два клиента на одном vault. Старые `.sync`, пакет 0.1.6.1080 и
предмиграционные архивы сохранены в приватном каталоге восстановления.

`make notes-sync-test backup-test` проверяет защиту bootstrap и упаковку
credentials без сети и production. Runtime дополнительно проверяется на
временных данных: изменения в обе стороны, конфликт, архивирование удалений,
отсутствующий marker и создание/изменение тестового файла через Яндекс API.
