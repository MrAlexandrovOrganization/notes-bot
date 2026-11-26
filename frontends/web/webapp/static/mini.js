"use strict";

const loginMessage = document.getElementById("mini-login");
const app = window.Telegram?.WebApp;
if (app) {
  app.ready();
  app.expand();
}

if (loginMessage.dataset.signedIn === "true") {
  window.location.replace("/day");
} else if (app?.initData) {
  loginMessage.textContent = "Вход через Telegram…";
  fetch("/mini/session", {
    method: "POST",
    body: new URLSearchParams({ init_data: app.initData }),
    credentials: "same-origin",
  }).then((response) => {
    if (response.status === 204) {
      window.location.replace("/day");
    } else {
      loginMessage.textContent = "Войдите с паролем Notes или заново откройте мини-приложение.";
    }
  }).catch(() => {
    loginMessage.textContent = "Не удалось войти через Telegram. Проверьте VPN.";
  });
}
