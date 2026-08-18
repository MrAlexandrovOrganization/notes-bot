package com.example.notes.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.example.notes.data.llm.LLMReminderResult
import notifications.Notifications.ScheduleParams
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

private val SCHEDULE_TYPES = listOf(
    "daily" to "Ежедневно",
    "weekly" to "По дням недели",
    "monthly" to "Каждый месяц",
    "yearly" to "Каждый год",
    "once" to "Один раз",
    "custom_days" to "Каждые N дней",
)

private val WEEKDAY_NAMES = listOf("Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс")

data class ReminderFormData(
    val title: String = "",
    val scheduleType: String = "daily",
    val hour: String = "",
    val minute: String = "",
    val days: Set<Int> = emptySet(),
    val dayOfMonth: String = "",
    val month: String = "",
    val day: String = "",
    val date: String = "",
    val intervalDays: String = "",
    val createTask: Boolean = false,
)

fun LLMReminderResult.toForm(): ReminderFormData = ReminderFormData(
    title = title,
    scheduleType = scheduleType.ifBlank { "daily" },
    hour = hour.toString(),
    minute = minute.toString(),
    days = days.toSet(),
    dayOfMonth = dayOfMonth.toString(),
    month = month.toString(),
    day = day.toString(),
    date = date,
    intervalDays = intervalDays.toString(),
    createTask = createTask,
)

/**
 * Mirrors the web frontend's formToScheduleParams validation rules and builds
 * the protobuf ScheduleParams for the notifications service.
 * Returns params + null error, or null + error string.
 */
fun buildScheduleParams(form: ReminderFormData, tzOffset: Int): Pair<ScheduleParams?, String?> {
    val hour = form.hour.trim().toIntOrNull()
    val minute = form.minute.trim().toIntOrNull()
    if (hour == null || minute == null || hour !in 0..23 || minute !in 0..59) {
        return null to "Время должно быть в формате ЧЧ:ММ"
    }

    val b = ScheduleParams.newBuilder()
        .setHour(hour)
        .setMinute(minute)
        .setTzOffset(tzOffset)

    when (form.scheduleType) {
        "daily" -> Unit
        "weekly" -> {
            if (form.days.isEmpty()) return null to "Выберите хотя бы один день недели"
            if (form.days.any { it !in 0..6 }) return null to "День недели должен быть от 0 (Пн) до 6 (Вс)"
            b.setWeekly(
                ScheduleParams.WeeklyExtra.newBuilder()
                    .addAllDays(form.days.sorted())
                    .build(),
            )
        }
        "monthly" -> {
            val d = form.dayOfMonth.trim().toIntOrNull()
            if (d == null || d !in 1..31) return null to "Число месяца должно быть от 1 до 31"
            b.setMonthly(ScheduleParams.MonthlyExtra.newBuilder().setDayOfMonth(d).build())
        }
        "yearly" -> {
            val m = form.month.trim().toIntOrNull()
            val d = form.day.trim().toIntOrNull()
            if (m == null || d == null || m !in 1..12 || d !in 1..31) {
                return null to "Укажите корректные месяц (1-12) и день (1-31)"
            }
            b.setYearly(ScheduleParams.YearlyExtra.newBuilder().setMonth(m).setDay(d).build())
        }
        "once" -> {
            if (form.date.isBlank()) return null to "Укажите дату"
            b.setOnce(ScheduleParams.OnceExtra.newBuilder().setDate(form.date).build())
        }
        "custom_days" -> {
            val n = form.intervalDays.trim().toIntOrNull()
            if (n == null || n < 1) return null to "Интервал должен быть положительным числом дней"
            b.setCustomDays(ScheduleParams.CustomDaysExtra.newBuilder().setIntervalDays(n).build())
        }
        else -> return null to "Неизвестный тип расписания"
    }
    return b.build() to null
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun ReminderFormDialog(
    tzOffset: Int,
    initial: ReminderFormData,
    onDismiss: () -> Unit,
    onSave: (ReminderFormData) -> Unit,
) {
    var form by remember { mutableStateOf(initial) }
    var error by remember { mutableStateOf<String?>(null) }
    var showDatePicker by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (initial.title.isBlank() && initial.scheduleType == "daily") "Новое напоминание" else "Напоминание") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = form.title,
                    onValueChange = { form = form.copy(title = it) },
                    label = { Text("Название") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )

                Text("Расписание", style = MaterialTheme.typography.labelMedium)
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    SCHEDULE_TYPES.forEach { (key, label) ->
                        FilterChip(
                            selected = form.scheduleType == key,
                            onClick = { form = form.copy(scheduleType = key) },
                            label = { Text(label) },
                        )
                    }
                }

                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = form.hour,
                        onValueChange = { form = form.copy(hour = it) },
                        label = { Text("Час") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                    )
                    OutlinedTextField(
                        value = form.minute,
                        onValueChange = { form = form.copy(minute = it) },
                        label = { Text("Минуты") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                    )
                }

                when (form.scheduleType) {
                    "weekly" -> {
                        Text("Дни недели", style = MaterialTheme.typography.labelMedium)
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            WEEKDAY_NAMES.forEachIndexed { i, name ->
                                FilterChip(
                                    selected = i in form.days,
                                    onClick = {
                                        val newDays = if (i in form.days) {
                                            form.days - i
                                        } else {
                                            form.days + i
                                        }
                                        form = form.copy(days = newDays)
                                    },
                                    label = { Text(name) },
                                )
                            }
                        }
                    }
                    "monthly" -> {
                        OutlinedTextField(
                            value = form.dayOfMonth,
                            onValueChange = { form = form.copy(dayOfMonth = it) },
                            label = { Text("Число месяца (1-31)") },
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    "yearly" -> {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(
                                value = form.month,
                                onValueChange = { form = form.copy(month = it) },
                                label = { Text("Месяц (1-12)") },
                                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            OutlinedTextField(
                                value = form.day,
                                onValueChange = { form = form.copy(day = it) },
                                label = { Text("День (1-31)") },
                                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                    "once" -> {
                        OutlinedTextField(
                            value = form.date,
                            onValueChange = { form = form.copy(date = it) },
                            label = { Text("Дата (ГГГГ-ММ-ДД)") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        TextButton(onClick = { showDatePicker = true }) {
                            Text("Выбрать дату")
                        }
                    }
                    "custom_days" -> {
                        OutlinedTextField(
                            value = form.intervalDays,
                            onValueChange = { form = form.copy(intervalDays = it) },
                            label = { Text("Интервал, дней") },
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }

                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = "Создать задачу в дневнике",
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.weight(1f),
                    )
                    Switch(
                        checked = form.createTask,
                        onCheckedChange = { form = form.copy(createTask = it) },
                    )
                }

                error?.let {
                    Text(
                        text = it,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    val (_, err) = buildScheduleParams(form, tzOffset)
                    if (err != null) {
                        error = err
                    } else {
                        onSave(form)
                    }
                },
            ) {
                Text("Сохранить")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text("Отмена")
            }
        },
    )

    if (showDatePicker) {
        val dateState = rememberDatePickerState()
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(
                    onClick = {
                        dateState.selectedDateMillis?.let { ms ->
                            val iso = Instant.ofEpochMilli(ms)
                                .atZone(ZoneOffset.UTC)
                                .toLocalDate()
                                .format(DateTimeFormatter.ISO_LOCAL_DATE)
                            form = form.copy(date = iso)
                        }
                        showDatePicker = false
                    },
                ) {
                    Text("ОК")
                }
            },
            dismissButton = {
                TextButton(onClick = { showDatePicker = false }) {
                    Text("Отмена")
                }
            },
        ) {
            DatePicker(state = dateState)
        }
    }
}