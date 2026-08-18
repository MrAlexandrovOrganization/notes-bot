package com.example.notes.data.llm

/**
 * Structured reminder data extracted by the LLM from a natural-language
 * description. Mirrors frontends/telegram/clients/llm.go's LLMReminderResult.
 */
data class LLMReminderResult(
    val title: String,
    val scheduleType: String, // daily/weekly/monthly/yearly/once/custom_days
    val hour: Int,
    val minute: Int,
    val days: List<Int>,       // weekly: 0=Mon…6=Sun
    val dayOfMonth: Int,       // monthly
    val month: Int,            // yearly
    val day: Int,              // yearly
    val date: String,          // once: YYYY-MM-DD
    val intervalDays: Int,     // custom_days
    val createTask: Boolean,
)