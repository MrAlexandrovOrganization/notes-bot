"use strict";

(() => {
  const selector = "form[data-note-draft]";
  const prefix = "notes.text-draft.v1:";
  const inFlight = new Set();
  const refreshes = new Map();
  let storage;
  try {
    storage = window.localStorage;
  } catch {
    return;
  }

  function draftKey(form) {
    const target = form.elements.namedItem("date") ?? form.elements.namedItem("relpath");
    return prefix + JSON.stringify([form.dataset.noteDraft, target.value]);
  }

  function showStatus(form, message) {
    const status = form.querySelector("[data-draft-status]");
    if (!status) return;
    status.textContent = message;
    status.hidden = message === "";
  }

  function storeDraft(form, text) {
    try {
      const key = draftKey(form);
      if (text) storage.setItem(key, text);
      else storage.removeItem(key);
      return true;
    } catch {
      showStatus(form, "Браузер не сохранил черновик. Скопируйте текст перед закрытием.");
      return false;
    }
  }

  function restoreDrafts() {
    for (const form of document.querySelectorAll(selector)) {
      const field = form.elements.namedItem("text");
      if (field.value !== "") continue;
      let text;
      try {
        text = storage.getItem(draftKey(form));
      } catch {
        return;
      }
      if (!text) continue;
      field.value = text;
      const details = form.closest("details");
      if (details) details.open = true;
      showStatus(form, "Черновик восстановлен");
    }
  }

  async function refreshNote(form, date) {
    if (!form.isConnected) return;
    const name = form.dataset.noteDraft === "task" ? "tasks" : "note";
    const selector = '[data-draft-refresh="' + name + '"]';
    const current = document.querySelector(selector);
    if (!current) return;

    refreshes.get(name)?.abort();
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15000);
    refreshes.set(name, controller);
    const source = new URL(location.href);
    if (date) source.searchParams.set("date", date);
    try {
      const response = await fetch(source, {
        cache: "no-store",
        signal: controller.signal,
      });
      if (!response.ok || response.redirected) return;
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      const replacement = page.querySelector(selector);
      if (!replacement || controller.signal.aborted || !current.isConnected) return;
      current.replaceChildren(...replacement.childNodes);
      window.htmx?.process(current);
    } catch {
      // The save has already been confirmed; a subsequent page load can refresh it.
    } finally {
      clearTimeout(timer);
      if (refreshes.get(name) === controller) refreshes.delete(name);
    }
  }

  document.addEventListener("input", (event) => {
    const form = event.target.form;
    if (!form?.matches(selector) || event.target.name !== "text") return;
    if (storeDraft(form, event.target.value)) showStatus(form, "");
  });

  document.addEventListener("submit", async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.matches(selector)) return;

    const field = form.elements.namedItem("text");
    const key = draftKey(form);
    if (inFlight.has(key)) {
      event.preventDefault();
      event.stopImmediatePropagation();
      return;
    }
    const text = field.value;
    const body = new URLSearchParams(new FormData(form));
    if (!storeDraft(form, text)) return; // Use the standard form if storage is unavailable.

    event.preventDefault();
    event.stopImmediatePropagation();
    inFlight.add(key);
    const buttons = Array.from(form.querySelectorAll('button[type="submit"]:enabled'));
    for (const button of buttons) button.disabled = true;
    showStatus(form, "Сохраняю…");
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 25000);
    try {
      const response = await fetch(form.action, {
        method: "POST",
        body,
        redirect: "manual",
        headers: {"X-Notes-Draft": "1"},
        signal: controller.signal,
      });
      if (response.status !== 204 || response.headers.get("X-Notes-Draft") !== "saved") {
        showStatus(form, "Сохранение не подтверждено. Черновик остался на устройстве.");
        return;
      }
      let cleared = false;
      try {
        if (storage.getItem(key) === text) {
          storage.removeItem(key);
          cleared = true;
        }
      } catch {
        // Keep existing local recovery data if storage has become inaccessible.
      }
      if (field.value === text) field.value = "";
      // A task/rating action may have replaced <main> during the request.
      const editor = Array.from(document.querySelectorAll(selector)).find((item) => draftKey(item) === key);
      if (editor) {
        const input = editor.elements.namedItem("text");
        if (cleared && input.value === text) input.value = "";
        if (!input.value) showStatus(editor, "Сохранено");
        await refreshNote(editor, body.get("date"));
      }
    } catch {
      showStatus(form, "Сохранение не подтверждено. Черновик остался на устройстве.");
    } finally {
      clearTimeout(timer);
      inFlight.delete(key);
      for (const button of buttons) button.disabled = false;
    }
  }, true);

  document.addEventListener("htmx:afterSwap", restoreDrafts);
  window.addEventListener("pageshow", restoreDrafts);
  restoreDrafts();
})();
