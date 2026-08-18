package com.example.notes.util

import io.grpc.Status
import io.grpc.StatusRuntimeException

/** Human-friendly Russian message for common gRPC/network failures. */
fun friendlyMessage(e: Throwable): String {
    if (e is StatusRuntimeException) {
        return when (e.status.code) {
            Status.Code.UNAVAILABLE ->
                "Сервис недоступен. Проверьте, что бэкенды запущены и адрес в strings.xml указан верно."
            Status.Code.DEADLINE_EXCEEDED -> "Превышено время ожидания сервера."
            Status.Code.UNIMPLEMENTED -> "Эта функция выключена на сервере."
            Status.Code.INVALID_ARGUMENT -> e.status.description ?: "Некорректные данные запроса."
            Status.Code.NOT_FOUND -> "Не найдено."
            else -> e.status.description ?: "Ошибка сервера (${e.status.code})."
        }
    }
    return e.message?.takeIf { it.isNotBlank() } ?: "Произошла ошибка."
}