package com.example.notes.data.llm

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.net.HttpURLConnection
import java.net.URL

/**
 * HTTP client for the Ollama /api/chat endpoint — used only for
 * natural-language reminder parsing. Replicates the Go LLMClient's
 * ParseReminder (same system prompt, JSON schema, think=false, num_predict).
 */
class LlmClient(
    private val host: String,
    private val port: Int,
    private val model: String,
) {

    suspend fun parseReminder(
        text: String,
        currentDateTime: String,
        today: String,
        tomorrow: String,
        dayAfter: String,
    ): LLMReminderResult = withContext(Dispatchers.IO) {
        // Go's fmt.Sprintf supports %[2]s indexed placeholders; Java's
        // Formatter does not. Substitute the indexed ones first, then format
        // the remaining sequential %s (the 4 header lines).
        val system = LLM_SYSTEM_PROMPT
            .replace("%[2]s", today)
            .replace("%[3]s", tomorrow)
            .replace("%[4]s", dayAfter)
            .format(currentDateTime, today, tomorrow, dayAfter)
        val content = chat(system, text, buildSchema(), numPredict = 256)
        parseResult(extractJson(content))
    }

    private fun chat(system: String, user: String, schema: JSONObject, numPredict: Int): String {
        val body = JSONObject().apply {
            put("model", model)
            put("stream", false)
            put("think", false)
            put("format", schema)
            put("options", JSONObject().apply {
                put("num_predict", numPredict)
                put("temperature", 0)
            })
            put("messages", JSONArray().apply {
                put(JSONObject().apply {
                    put("role", "system")
                    put("content", system)
                })
                put(JSONObject().apply {
                    put("role", "user")
                    put("content", user)
                })
            })
        }

        val url = URL("http://$host:$port/api/chat")
        val conn = url.openConnection() as HttpURLConnection
        try {
            conn.requestMethod = "POST"
            conn.connectTimeout = 30_000
            conn.readTimeout = 5 * 60_000
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "application/json")
            conn.outputStream.use { it.write(body.toString().toByteArray()) }

            val status = conn.responseCode
            if (status != HttpURLConnection.HTTP_OK) {
                throw RuntimeException("Ollama вернул статус $status")
            }
            val responseText = BufferedReader(conn.inputStream.reader()).use { it.readText() }
            val obj = JSONObject(responseText)
            return THINK_REGEX.replace(obj.getJSONObject("message").getString("content"), "")
                .trim()
        } finally {
            conn.disconnect()
        }
    }

    private fun parseResult(json: String): LLMReminderResult {
        val obj = JSONObject(json)
        val days = obj.optJSONArray("days")
        return LLMReminderResult(
            title = obj.optString("title"),
            scheduleType = obj.optString("schedule_type"),
            hour = obj.optInt("hour"),
            minute = obj.optInt("minute"),
            days = if (days != null) {
                (0 until days.length()).map { days.optInt(it) }
            } else {
                emptyList()
            },
            dayOfMonth = obj.optInt("day_of_month"),
            month = obj.optInt("month"),
            day = obj.optInt("day"),
            date = obj.optString("date"),
            intervalDays = obj.optInt("interval_days"),
            createTask = obj.optBoolean("create_task"),
        )
    }

    private fun extractJson(s: String): String {
        val start = s.indexOf('{')
        val end = s.lastIndexOf('}')
        if (start == -1 || end == -1 || end < start) return s
        return s.substring(start, end + 1)
    }

    private fun buildSchema(): JSONObject = JSONObject().apply {
        put("type", "object")
        put("properties", JSONObject().apply {
            put("title", JSONObject().put("type", "string"))
            put("schedule_type", JSONObject().apply {
                put("type", "string")
                put("enum", JSONArray().apply {
                    put("daily")
                    put("weekly")
                    put("monthly")
                    put("yearly")
                    put("once")
                    put("custom_days")
                })
            })
            put("hour", JSONObject().apply {
                put("type", "integer")
                put("minimum", 0)
                put("maximum", 23)
            })
            put("minute", JSONObject().apply {
                put("type", "integer")
                put("minimum", 0)
                put("maximum", 59)
            })
            put("days", JSONObject().apply {
                put("type", "array")
                put("items", JSONObject().put("type", "integer"))
            })
            put("day_of_month", JSONObject().apply {
                put("type", "integer")
                put("minimum", 1)
                put("maximum", 31)
            })
            put("month", JSONObject().apply {
                put("type", "integer")
                put("minimum", 1)
                put("maximum", 12)
            })
            put("day", JSONObject().apply {
                put("type", "integer")
                put("minimum", 1)
                put("maximum", 31)
            })
            put("date", JSONObject().put("type", "string"))
            put("interval_days", JSONObject().apply {
                put("type", "integer")
                put("minimum", 1)
            })
            put("create_task", JSONObject().put("type", "boolean"))
        })
        put("required", JSONArray().apply {
            put("title")
            put("schedule_type")
            put("hour")
            put("minute")
            put("days")
            put("day_of_month")
            put("interval_days")
            put("date")
            put("create_task")
        })
    }

    private companion object {
        val THINK_REGEX = Regex("""(?is)<thinking>.*?</thinking>""")

        val LLM_SYSTEM_PROMPT = """Сейчас: %s
Сегодня (для слов "сегодня", "сегодня вечером"): %s
Завтра (для слов "завтра"): %s
Послезавтра (для слов "послезавтра"): %s

Ты помощник для разбора напоминаний. Из текста пользователя извлеки параметры и верни JSON.

Поля:
- title: короткое название (без слов "напоминай", "каждый" и т.п.)
- schedule_type: ОДНО из значений: "daily", "weekly", "monthly", "yearly", "once", "custom_days"
- hour, minute: время в 24-часовом формате (целые числа)
- days: ОБЯЗАТЕЛЬНО для weekly — массив номеров дней [0=Пн, 1=Вт, 2=Ср, 3=Чт, 4=Пт, 5=Сб, 6=Вс]. Для остальных типов — пустой массив [].
- day_of_month: ОБЯЗАТЕЛЬНО для monthly — число месяца (1–31). Для остальных — 0.
- interval_days: ОБЯЗАТЕЛЬНО для custom_days — интервал в днях. Для остальных — 0.
- month, day: для yearly — месяц (1–12) и число (1–31).
- date: для once — РЕАЛЬНАЯ дата в формате YYYY-MM-DD (например 2026-06-29). Никогда не возвращай шаблоны типа "<завтра>" или "<сегодня + 1>".
  Если пользователь сказал "завтра" — подставь значение поля Завтра выше дословно. Если "послезавтра" — Послезавтра.
  Если назван месяц без года — используй ближайшую будущую дату (год берётся из Сегодня).
  Дата ВСЕГДА должна быть >= Сегодня. Если получается прошлая дата — увеличь год на 1.
- create_task: true только если явно просят создать задачу.

Месяцы: январь=1, февраль=2, март=3, апрель=4, май=5, июнь=6, июль=7, август=8, сентябрь=9, октябрь=10, ноябрь=11, декабрь=12

Примеры (для контекста Сегодня=%[2]s, Завтра=%[3]s, Послезавтра=%[4]s):
- "каждый понедельник в 9" → schedule_type="weekly", days=[0], hour=9, minute=0
- "каждый пн и пт в 8:30" → schedule_type="weekly", days=[0,4], hour=8, minute=30
- "по будням в 8 утра" → schedule_type="weekly", days=[0,1,2,3,4], hour=8, minute=0
- "по выходным в 10" → schedule_type="weekly", days=[5,6], hour=10, minute=0
- "25 числа каждого месяца в 10" → schedule_type="monthly", day_of_month=25, hour=10, minute=0
- "каждое 20-е число в 15:00" → schedule_type="monthly", day_of_month=20, hour=15, minute=0
- "каждые 3 дня в 7 утра" → schedule_type="custom_days", interval_days=3, hour=7, minute=0
- "каждый год 2-го июня в 20:00" → schedule_type="yearly", month=6, day=2, hour=20, minute=0
- "каждое 8 марта в 9:00" → schedule_type="yearly", month=3, day=8, hour=9, minute=0
- "завтра в 8:00" → schedule_type="once", date="%[3]s", hour=8, minute=0
- "послезавтра в 14:00" → schedule_type="once", date="%[4]s", hour=14, minute=0
- "сегодня в 23:00" → schedule_type="once", date="%[2]s", hour=23, minute=0"""
    }
}