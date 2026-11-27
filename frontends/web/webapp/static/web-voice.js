"use strict";

(() => {
  const maxAudioBytes = 16 * 1024 * 1024;
  const composerSelector = "[data-voice-composer]";
  const contentSelector = "[data-voice-note-content]";
  const activeStatuses = new Set(["queued", "transcribing"]);
  const serverStatuses = new Set([...activeStatuses, "saved", "failed"]);
  const labels = {
    local: "Аудио сохранено на устройстве. Ожидает отправки.",
    queued: "Аудио отправлено. Ожидает расшифровки.",
    transcribing: "Расшифровываем аудио…",
    saved: "Расшифровка добавлена в заметку.",
    failed: "Не удалось обработать аудио.",
  };
  const messages = new Map();
  const queueMessages = new Map();
  const retryAfter = new Map();
  let jobs = [];
  let database;
  let recording;
  let working = false;
  let runAgain = false;

  function openDatabase() {
    database ??= new Promise((resolve, reject) => {
      const request = indexedDB.open("notes-web-voice", 1);
      request.onupgradeneeded = () => request.result.createObjectStore("jobs", { keyPath: "id" });
      request.onsuccess = () => {
        const db = request.result;
        db.onversionchange = () => {
          db.close();
          database = undefined;
        };
        resolve(db);
      };
      request.onerror = () => reject(request.error);
    }).catch((error) => {
      database = undefined;
      throw error;
    });
    return database;
  }

  async function withJobs(mode, operation) {
    const db = await openDatabase();
    return new Promise((resolve, reject) => {
      const transaction = db.transaction("jobs", mode);
      const request = operation(transaction.objectStore("jobs"));
      transaction.oncomplete = () => resolve(request.result);
      transaction.onabort = () => reject(transaction.error);
    });
  }

  function updateJob(id, change) {
    return withJobs("readwrite", (store) => {
      const request = store.get(id);
      request.onsuccess = () => {
        const job = request.result;
        if (!job) return;
        const updated = change(job);
        if (updated) store.put(updated);
        else store.delete(id);
      };
      return request;
    });
  }

  function destination(composer) {
    return { date: composer.dataset.date, path: composer.dataset.path };
  }

  function key(target) {
    return target.date ? "date:" + target.date : "path:" + target.path;
  }

  function activeComposer(target, root = document) {
    return [...root.querySelectorAll(composerSelector)].find((composer) => key(destination(composer)) === key(target));
  }

  function showMessage(composer, message) {
    messages.set(key(destination(composer)), message);
    paint(composer);
  }

  function paint(composer) {
    composer.hidden = false;
    const target = destination(composer);
    const message = composer.querySelector("[data-voice-message]");
    const liveRecording = recording?.key === key(target) ? recording : null;
    const elapsed = liveRecording?.started ? Math.floor((performance.now() - liveRecording.started) / 1000) : 0;
    const recordButton = composer.querySelector("[data-voice-record]");
    const cancelButton = composer.querySelector("[data-voice-cancel]");
    const timer = composer.querySelector("[data-voice-timer]");
    const recordingLabel = liveRecording?.started ? "Отправить запись" : "Записать голосовую";

    recordButton.setAttribute("aria-label", recordingLabel);
    recordButton.title = recordingLabel;
    recordButton.setAttribute("aria-busy", String(Boolean(liveRecording && !liveRecording.started)));
    recordButton.querySelector("[data-voice-send]").toggleAttribute("hidden", !liveRecording?.started);
    recordButton.querySelector("[data-voice-mic]").toggleAttribute("hidden", Boolean(liveRecording?.started));
    cancelButton.hidden = !liveRecording;
    timer.hidden = !liveRecording?.started;
    timer.textContent = Math.floor(elapsed / 60) + ":" + String(elapsed % 60).padStart(2, "0");

    const recent = jobs.filter((job) => key(job) === key(target));
    const text = messages.get(key(target)) || recent.map((job) => queueMessages.get(job.id)).find(Boolean) || "";
    if (message.textContent !== text) message.textContent = text;
    message.hidden = !text;

    const list = composer.querySelector("[data-voice-jobs]");
    const version = JSON.stringify(recent.map(({ id, status, error }) => [id, status, error]));
    if (list.dataset.version === version) return;
    list.dataset.version = version;
    list.hidden = recent.length === 0;
    list.replaceChildren(...recent.map((job) => {
      const row = document.createElement("li");
      row.textContent = job.error || labels[job.status];
      if (job.status === "failed") {
        const dismiss = document.createElement("button");
        dismiss.type = "button";
        dismiss.textContent = "Закрыть";
        dismiss.dataset.voiceDismiss = job.id;
        row.append(dismiss);
      }
      return row;
    }));
  }

  function paintAll() {
    document.querySelectorAll(composerSelector).forEach(paint);
  }

  function newID() {
    if (crypto.randomUUID) return crypto.randomUUID();
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
    return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join("-");
  }

  async function addAudio(composer, blob) {
    if (!blob?.size || blob.size > maxAudioBytes) {
      showMessage(composer, blob?.size ? "Выберите аудио до 16 МиБ." : "Запись пуста. Попробуйте ещё раз.");
      return;
    }
    try {
      const job = { ...destination(composer), id: newID(), blob, status: "local" };
      await withJobs("readwrite", (store) => store.add(job));
      jobs.push(job);
      showMessage(composer, "");
      void processJobs();
    } catch {
      showMessage(composer, "Не удалось сохранить аудио на устройстве. Проверьте доступ к хранилищу.");
    }
  }

  function request(url, options, timeout, readResponse) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeout);
    return fetch(url, { ...options, credentials: "same-origin", cache: "no-store", redirect: "manual", signal: controller.signal })
      .then(readResponse)
      .finally(() => clearTimeout(timer));
  }

  async function updateFromServer(job) {
    const url = "/voice/" + encodeURIComponent(job.id);
    let options = { method: "GET" };
    if (job.blob) {
      const body = new FormData();
      body.append("audio", job.blob, "audio");
      if (job.date) body.append("date", job.date);
      else body.append("path", job.path);
      options = { method: "PUT", body };
    }
    const { response, data } = await request(url, options, job.blob ? 120000 : 15000, async (response) => {
      const json = response.headers.get("Content-Type")?.includes("application/json");
      return { response, data: json ? await response.json() : null };
    });
    queueMessages.delete(job.id);
    if (response.ok && data?.id === job.id && serverStatuses.has(data.status)) {
      await updateJob(job.id, (current) => {
        if (["saved", "failed"].includes(current.status)) return current;
        return { ...current, blob: undefined, status: data.status, error: data.error || "" };
      });
      return;
    }

    const errors = {
      400: "Не удалось прочитать аудио. Выберите MP4, WebM, Ogg, MP3, AAC, WAV или FLAC.",
      404: "Задание отсутствует на сервере.",
      409: "Сервер уже принял другую запись с этим идентификатором.",
      413: "Выберите аудио до 16 МиБ.",
    };
    if (errors[response.status]) {
      await updateJob(job.id, (current) => (
        ["saved", "failed"].includes(current.status)
          ? current
          : { ...current, status: "failed", error: errors[response.status] }
      ));
      return;
    }
    if (response.type === "opaqueredirect" || response.status === 401 || response.status === 403) {
      queueMessages.set(job.id, "Войдите в Notes, чтобы продолжить отправку и расшифровку.");
      return;
    }
    queueMessages.set(job.id, "Сервер временно не принимает аудио. Повторим отправку.");
    const delay = Number(response.headers.get("Retry-After")) || 10;
    retryAfter.set(job.id, Date.now() + delay * 1000);
  }

  async function refreshSavedNote(job) {
    const composer = activeComposer(job);
    const content = document.querySelector(contentSelector);
    if (!composer || !content) return;
    const url = window.location.href;
    const html = await request(url, {}, 15000, (response) => response.ok ? response.text() : "");
    if (!html) return;
    const page = new DOMParser().parseFromString(html, "text/html");
    const updated = page.querySelector(contentSelector);
    if (!updated || !activeComposer(job, page) || window.location.href !== url ||
        composer !== activeComposer(job) || !content.isConnected) return;
    content.replaceWith(updated);
    await updateJob(job.id, () => null);
    showMessage(composer, labels.saved);
  }

  async function processJobs() {
    if (working) {
      runAgain = true;
      return;
    }
    working = true;
    try {
      jobs = await withJobs("readonly", (store) => store.getAll());
      paintAll();
      for (const job of jobs) {
        if (job.status === "saved") {
          await refreshSavedNote(job).catch(() => {});
          continue;
        }
        if (job.status !== "local" && !activeStatuses.has(job.status)) continue;
        if ((retryAfter.get(job.id) || 0) > Date.now()) continue;
        retryAfter.delete(job.id);
        try {
          await updateFromServer(job);
        } catch {
          queueMessages.set(job.id, "Нет связи с Notes. Продолжим после подключения.");
        }
      }
      jobs = await withJobs("readonly", (store) => store.getAll());
      paintAll();
    } catch {
      document.querySelectorAll(composerSelector).forEach((composer) => {
        showMessage(composer, "Хранилище аудио недоступно в этом браузере.");
      });
    } finally {
      working = false;
      if (runAgain) {
        runAgain = false;
        void processJobs();
      }
    }
  }

  function releaseRecording(state) {
    clearInterval(state.timer);
    state.stream?.getTracks().forEach((track) => track.stop());
    if (recording === state) recording = undefined;
    paintAll();
  }

  function stopRecording(state, cancelled) {
    state.cancelled ||= cancelled;
    if (state.recorder?.state === "recording") {
      state.recorder.stop();
    } else if (!state.recorder) {
      releaseRecording(state);
    }
  }

  function makeRecorder(stream) {
    const types = ["audio/mp4", "audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus"];
    for (const mimeType of types) {
      if (!MediaRecorder.isTypeSupported?.(mimeType)) continue;
      try { return new MediaRecorder(stream, { mimeType }); }
      catch { /* Try another supported container or the browser default. */ }
    }
    return new MediaRecorder(stream);
  }

  async function toggleRecording(composer) {
    const target = destination(composer);
    if (recording) {
      if (recording.key === key(target) && recording.started) stopRecording(recording, false);
      return;
    }
    if (!window.MediaRecorder || !navigator.mediaDevices?.getUserMedia) {
      showMessage(composer, "Микрофон недоступен. Выберите аудиофайл.");
      composer.querySelector("[data-voice-file]").click();
      return;
    }
    const state = { key: key(target) };
    recording = state;
    showMessage(composer, "");
    try {
      state.stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (state.cancelled || !activeComposer(target)) {
        releaseRecording(state);
        return;
      }
      state.recorder = makeRecorder(state.stream);
      const chunks = [];
      let size = 0;
      state.recorder.ondataavailable = (event) => {
        if (!event.data.size || state.cancelled) return;
        size += event.data.size;
        if (size > maxAudioBytes) {
          stopRecording(state, true);
          showMessage(activeComposer(target) || composer, "Достигнут предел 16 МиБ. Запишите более короткое аудио.");
          return;
        }
        chunks.push(event.data);
      };
      state.recorder.onstop = () => {
        const current = activeComposer(target) || composer;
        releaseRecording(state);
        if (!state.cancelled) {
          const type = state.recorder.mimeType || chunks[0]?.type || "";
          void addAudio(current, new Blob(chunks, { type }));
        }
      };
      state.recorder.onerror = () => {
        state.cancelled = true;
        releaseRecording(state);
        showMessage(activeComposer(target) || composer, "Не удалось записать аудио. Выберите аудиофайл.");
      };
      state.recorder.start(1000);
      state.started = performance.now();
      state.timer = setInterval(paintAll, 500);
      paintAll();
    } catch {
      releaseRecording(state);
      showMessage(activeComposer(target) || composer, "Нет доступа к микрофону. Выберите аудиофайл.");
      activeComposer(target)?.querySelector("[data-voice-pick]").focus();
    }
  }

  document.addEventListener("click", async (event) => {
    const composer = event.target.closest(composerSelector);
    if (!composer) return;
    if (event.target.closest("[data-voice-record]")) void toggleRecording(composer);
    if (event.target.closest("[data-voice-cancel]") && recording) stopRecording(recording, true);
    if (event.target.closest("[data-voice-pick]")) composer.querySelector("[data-voice-file]").click();
    const id = event.target.closest("[data-voice-dismiss]")?.dataset.voiceDismiss;
    if (id) {
      try {
        await updateJob(id, () => null);
        void processJobs();
      } catch {
        showMessage(composer, "Не удалось удалить запись из хранилища браузера.");
      }
    }
  });

  document.addEventListener("change", (event) => {
    if (!event.target.matches("[data-voice-file]")) return;
    const file = event.target.files?.[0];
    event.target.value = "";
    if (file) void addAudio(event.target.closest(composerSelector), file);
  });

  document.addEventListener("dragover", (event) => {
    const composer = event.target.closest(composerSelector);
    if (!composer || !event.dataTransfer?.types.includes("Files")) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
    composer.setAttribute("data-voice-drop", "");
  });
  document.addEventListener("dragleave", (event) => {
    const composer = event.target.closest(composerSelector);
    if (composer && !composer.contains(event.relatedTarget)) composer.removeAttribute("data-voice-drop");
  });
  document.addEventListener("drop", (event) => {
    const composer = event.target.closest(composerSelector);
    if (!composer || !event.dataTransfer?.files.length) return;
    event.preventDefault();
    composer.removeAttribute("data-voice-drop");
    if (event.dataTransfer.files.length === 1) void addAudio(composer, event.dataTransfer.files[0]);
    else showMessage(composer, "Перетащите один аудиофайл.");
  });

  document.addEventListener("htmx:afterSwap", () => {
    if (recording && ![...document.querySelectorAll(composerSelector)].some((composer) => key(destination(composer)) === recording.key)) {
      stopRecording(recording, true);
    }
    paintAll();
    void processJobs();
  });

  window.addEventListener("pagehide", () => {
    if (recording) stopRecording(recording, true);
  });
  window.addEventListener("pageshow", processJobs);
  window.addEventListener("online", processJobs);
  setInterval(processJobs, 5000);
  paintAll();
  void processJobs();
})();
