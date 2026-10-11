# Проверка CPU и GPU воркеров в WSL

12 октября 2026 года через `ssh wsl` проверен настоящий Whisper inference
на CPU и NVIDIA GPU, затем оба процесса запущены одновременно. Production,
Telegram, серверная очередь и работающая Ollama в проверке не участвовали.

## Окружение

| Компонент | Подтверждённое значение |
|---|---|
| ОС | Ubuntu 22.04.5 LTS, WSL2, x86_64 |
| CPU / RAM | Intel i7-12650H, 16 логических CPU, около 15 GiB RAM |
| GPU | NVIDIA GeForce MX550, 2 GiB VRAM, compute capability 7.5 |
| Драйвер / CUDA | 572.42 / системный toolkit и cuBLAS 12.8 |
| Python | 3.11.15, отдельный venv |
| Inference | faster-whisper 1.2.1, CTranslate2 4.7.1, PyAV 17.0.0, onnxruntime 1.24.4 |
| cuDNN | `nvidia-cudnn-cu12==9.8.0.87` только в venv; runtime API вернул 90800 |
| Модель | `Systran/faster-whisper-base`, revision `ebe41f70d5b6dfa9166e2c581c45c9c0cfc57b66` |

Версии inference-зависимостей взяты из `backends/transcriber/poetry.lock`:
из него извлечены constraints для всех 54 пакетов, а pip установил нужное
подмножество. cuDNN установлен с `--no-deps`, поскольку совместимый cuBLAS
уже есть системно. Это связь с системной CUDA, которую следует учитывать при
воспроизведении на другой машине; один `pip freeze` её не описывает.

Проверка использовала точную копию текущего
`backends/transcriber/whisper/inference.py`, preset `voice`, language override
`en`, CPU threads `2`, local-files-only и offline Hugging Face. Модель скачана
заранее с указанной ревизии. Синтетическое аудио `speech.aiff` создано на Mac
командой `say`, содержит фразу: «The worker is ready. This is a local
transcription test.». Во всех запусках получено: «The worker is ready, this as
a local transcription test.».

## Результаты

| Режим | Загрузка модели | Три распознавания | Пиковый RSS процесса |
|---|---:|---|---:|
| CPU / int8 | 0.291 s | 0.716 / 0.665 / 0.632 s | 398.8 MiB |
| CUDA / int8_float16 | 0.430 s | 0.785 / 0.344 / 0.330 s | 584.3 MiB |

CTranslate2 непосредственно сообщил поддержку CUDA compute types:
`float16`, `float32`, `int8`, `int8_float16`, `int8_float32`.
GPU execution прошёл с `int8_float16`, включая загрузку cuDNN и реальное
потребление VRAM. Пиковая общая занятость GPU по выборкам `nvidia-smi` —
435 MiB при исходных 170 MiB; наблюдавшаяся максимальная загрузка — 74%.
Это общие метрики устройства, включая Windows, а не точный профиль одного
процесса. Частота измерений около 0.2 s, кратковременные пики могли быть выше.

При одновременном запуске одного CPU- и одного GPU-процесса все шесть
распознаваний завершились успешно. CPU: 0.906 / 0.956 / 0.685 s;
GPU: 0.593 / 0.359 / 0.341 s. Общий wall time с загрузкой моделей — 3.053 s.
CPU использовал два потока, GPU concurrency оставалась равной одному.
После завершения VRAM вернулась к 170 MiB, Ollama осталась active,
swap — 0 из 4 GiB, доступно около 14 GiB RAM.

## Как повторить текущий smoke test

Окружение и скрипты сохранены на WSL в `/tmp/transcriber-wsl-pilot`.
Venv занимает около 1.5 GiB, модель — 142 MiB. Каталог `/tmp` может быть
очищен при перезапуске; это временный стенд, а не постоянная установка worker.
В каталоге находятся `inference.py`, `speech.aiff`,
`wsl-whisper-inference-smoke.py`, `requirements-pilot.lock.txt` и отчёты JSON.

GPU:

```bash
ssh wsl 'cd /tmp/transcriber-wsl-pilot &&
LD_LIBRARY_PATH=/tmp/transcriber-wsl-pilot/venv/lib/python3.11/site-packages/nvidia/cudnn/lib:/usr/local/cuda/targets/x86_64-linux/lib:/usr/lib/wsl/lib \
HF_HUB_OFFLINE=1 HF_HUB_DISABLE_IMPLICIT_TOKEN=1 \
venv/bin/python -u wsl-whisper-inference-smoke.py cuda'
```

CPU:

```bash
ssh wsl 'cd /tmp/transcriber-wsl-pilot &&
HF_HUB_OFFLINE=1 HF_HUB_DISABLE_IMPLICIT_TOKEN=1 \
venv/bin/python -u wsl-whisper-inference-smoke.py cpu'
```

Каждая команда запускает три распознавания и завершает процесс. Постоянные
службы worker не устанавливались. Системный Python, GPU-драйверы, Docker и
Ollama не менялись. Зависимости и модель устанавливались только в изолированный
стенд, без пользовательских данных и Telegram credentials.

## Границы вывода и следующий шаг

Проверка подтверждает inference engine на этом устройстве, включая совместную
работу CPU+GPU. Она не проверяет remote WorkerService/gRPC, lease recovery,
поведение при сне/отключении ноутбука, долговечность доставки в notes-bot,
качество русского языка или производительность длинных записей. Значения
времени для короткого синтетического аудио нельзя переносить на большие файлы
и модели `small`/`large` без измерений.

Для первого рабочего пилота подходит один CUDA worker с этой моделью и один
CPU worker с двумя потоками. Приоритет GPU над CPU требует правил выдачи задач
на ingress. Увеличивать GPU concurrency при 2 GiB VRAM следует только после
измерения нагрузки. Постоянную установку зависимостей, доступ через SSH-туннель
и полный цикл claim/download/heartbeat/complete нужно оформить отдельно.
До durable delivery ledger notes-bot нельзя считать длительное ожидание
отсутствующего worker полностью восстановимым после рестартов frontend.

Официальные основания выбора runtime:
[faster-whisper 1.2.1](https://github.com/SYSTRAN/faster-whisper/blob/v1.2.1/README.md),
[CTranslate2 hardware](https://opennmt.net/CTranslate2/hardware_support.html),
[compute types](https://opennmt.net/CTranslate2/quantization.html).
