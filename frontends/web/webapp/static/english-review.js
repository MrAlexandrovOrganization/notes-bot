"use strict";

(() => {
  const review = document.querySelector("[data-english-review]");
  if (!review) return;

  const scope = review.dataset.scope;
  const cardPanel = review.querySelector("[data-review-card]");
  const empty = review.querySelector("[data-review-empty]");
  const status = review.querySelector("[data-review-status]");
  const reveal = cardPanel.querySelector("details");
  const showAnswer = reveal.querySelector("summary");
  showAnswer.addEventListener("click", (event) => {
    if (event.detail === 0) {
      requestAnimationFrame(() => {
        if (reveal.open) reveal.querySelector("button").focus({ preventScroll: true });
      });
    }
  });
  const progress = cardPanel.querySelector("progress");
  const tokens = cardPanel.querySelectorAll('input[name="token"]');
  const keyPrefix = "notes.english.grade.";
  const storage = getStorage();
  let positions = JSON.parse(document.getElementById("english-round").textContent) ?? [];
  // The ordinary form remains available if the browser blocks storage or the
  // server could only provide the current card.
  if (!storage || (positions.length === 0 && !cardPanel.hidden)) return;

  let needsRound = false;
  let completed = false;
  let syncing = false;
  let conflicts = 0;
  let problem = "";
  let retryTimer;
  let retryDelay = 3000;
  let lastQueuedAt = pending().at(-1)?.queuedAt ?? 0;

  function getStorage() {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  }

  function pending() {
    return Object.keys(storage)
      .filter((key) => key.startsWith(keyPrefix))
      .map((key) => ({ key, ...JSON.parse(storage.getItem(key)) }))
      .filter((entry) => entry.token)
      .sort((a, b) => a.queuedAt - b.queuedAt || a.position - b.position);
  }

  function updateStatus() {
    const queued = pending().length;
    status.textContent = [
      queued ? `Ожидают отправки: ${queued}` : "",
      conflicts ? `Оценки отклонены сервером: ${conflicts}` : "",
      problem,
    ].filter(Boolean).join(". ");
    status.hidden = !status.textContent;
  }

  function showCurrent(focus = false) {
    const queued = new Set(pending().filter((grade) => grade.scope === scope).map((grade) => grade.token));
    const before = positions.length;
    while (positions.length && queued.has(positions[0].token)) positions.shift();
    if (before && !positions.length) {
      completed = true;
      needsRound = true;
    }

    const current = needsRound ? undefined : positions[0];
    cardPanel.hidden = !current;
    empty.hidden = !!current;
    if (!current) {
      empty.textContent = completed ? "Круг пройден" :
        needsRound ? "Обновляем карточки" : "Пока нет карточек";
      return;
    }

    const card = current.card;
    const russianFirst = current.prompt_language === "ru";
    cardPanel.querySelector("[data-review-position]").textContent =
      `Круг ${current.round} · Карточка ${current.position} из ${current.round_size}`;
    progress.max = Math.max(1, current.round_size);
    progress.value = current.position;
    cardPanel.querySelector("[data-review-instruction]").textContent =
      russianFirst ? "Вспомни по-английски" : "Вспомни по-русски";
    cardPanel.querySelector("[data-review-prompt]").textContent = russianFirst ? card.source : card.english;
    cardPanel.querySelector("[data-review-answer]").textContent = russianFirst ? card.english : card.source;
    for (const detail of cardPanel.querySelectorAll("[data-review-detail]")) {
      detail.textContent = card[detail.dataset.reviewDetail] ?? "";
      detail.hidden = !detail.textContent;
    }
    const changed = tokens[0].value !== current.token;
    for (const token of tokens) token.value = current.token;
    if (changed) reveal.open = false;
    if (focus) showAnswer.focus({ preventScroll: true });
  }

  function scopeParams(value) {
    const params = new URLSearchParams();
    if (value && value !== "all") {
      for (const id of value.split("+")) params.append("scope", id);
    }
    return params;
  }

  async function request(path, options = {}) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 25000);
    try {
      return await fetch(path, {
        ...options,
        headers: { Accept: "application/json" },
        signal: controller.signal,
      });
    } finally {
      clearTimeout(timeout);
    }
  }

  async function synchronize() {
    if (syncing) return;
    clearTimeout(retryTimer);
    syncing = true;
    try {
      let entry;
      while ((entry = pending()[0])) {
        const body = scopeParams(entry.scope);
        body.set("token", entry.token);
        body.set("action", entry.action);
        const response = await request("/english/grade", { method: "POST", body });
        if (response.status === 204) {
          storage.removeItem(entry.key);
        } else if ([400, 404, 409].includes(response.status)) {
          storage.removeItem(entry.key);
          conflicts++;
          if (entry.scope === scope) {
            needsRound = true;
            showCurrent();
          }
        } else {
          throw new Error(response.redirected ? "Войдите в Notes. Оценки сохранены в браузере." :
            "Не удалось отправить оценки. Они сохранены в браузере.");
        }
        problem = "";
        updateStatus();
      }

      if (needsRound) {
        const response = await request(`/english/round?${scopeParams(scope)}`);
        if (response.status === 404 && scope) {
          window.location.replace("/english");
          return;
        }
        if (response.redirected || !response.ok) throw new Error("Не удалось загрузить следующий набор карточек.");
        const data = await response.json();
        positions = data.positions ?? [];
        if (positions.length) completed = false;
        needsRound = false;
        showCurrent();
      }
      problem = "";
      retryDelay = 3000;
    } catch (error) {
      problem = error instanceof TypeError || error.name === "AbortError" ?
        "Нет связи с Notes. Оценки сохранены в браузере." : error.message;
      retryTimer = setTimeout(synchronize, retryDelay);
      retryDelay = Math.min(retryDelay * 2, 30000);
    } finally {
      syncing = false;
      updateStatus();
    }
  }

  review.addEventListener("submit", (event) => {
    const current = positions[0];
    const action = event.submitter?.value;
    if (needsRound || !current || !action) {
      event.preventDefault();
      return;
    }

    const token = new FormData(event.target).get("token");
    if (token !== current.token) {
      event.preventDefault();
      return;
    }
    try {
      const key = `${keyPrefix}${token}.${action}`;
      if (!storage.getItem(key)) {
        lastQueuedAt = Math.max(Date.now(), lastQueuedAt + 1);
        storage.setItem(key, JSON.stringify({
          scope, token, action, position: current.position, queuedAt: lastQueuedAt,
        }));
      }
    } catch {
      // With an empty queue the server-rendered form can still submit safely.
      if (!pending().length) return;
      event.preventDefault();
      problem = "Не удалось записать оценку в браузере. Попробуйте ещё раз.";
      updateStatus();
      return;
    }
    event.preventDefault();
    const focus = event.target.contains(document.activeElement);
    showCurrent(focus);
    updateStatus();
    void synchronize();
  });

  window.addEventListener("online", synchronize);
  window.addEventListener("pageshow", synchronize);
  window.addEventListener("storage", (event) => {
    if (!event.key?.startsWith(keyPrefix)) return;
    if (event.newValue) {
      showCurrent();
      void synchronize();
    }
    updateStatus();
  });

  showCurrent();
  updateStatus();
  void synchronize();
})();
