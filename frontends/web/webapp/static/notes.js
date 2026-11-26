"use strict";

// Keep the main region visible when a form update is rejected.
document.addEventListener("htmx:responseError", (event) => {
  const target = document.getElementById("form-error");
  if (target && !target.textContent.trim()) {
    target.textContent = "Не удалось сохранить изменения. Попробуйте ещё раз.";
  }
});
document.addEventListener("htmx:sendError", () => {
  const target = document.getElementById("form-error");
  if (target) target.textContent = "Нет связи с Notes. Проверьте VPN и попробуйте ещё раз.";
});
document.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail.target.matches("main")) {
    const target = document.getElementById("form-error");
    if (target) target.textContent = "";
  }
});
