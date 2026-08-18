package com.example.notes.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.example.notes.data.grpc.NotificationsClient
import com.example.notes.data.llm.LlmClient
import com.example.notes.ui.components.EmptyView
import com.example.notes.ui.components.ErrorView
import com.example.notes.ui.components.LoadingView
import com.example.notes.ui.components.ScreenState
import com.example.notes.util.Dates
import com.example.notes.util.friendlyMessage
import kotlinx.coroutines.launch
import notifications.Notifications.Reminder
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit

private val SCHEDULE_LABELS = mapOf(
    "daily" to "каждый день",
    "weekly" to "по дням недели",
    "monthly" to "каждый месяц",
    "yearly" to "каждый год",
    "once" to "один раз",
    "custom_days" to "каждые N дней",
)

private fun scheduleLabel(type: String): String = SCHEDULE_LABELS[type] ?: type

@Composable
fun RemindersScreen(
    notifications: NotificationsClient,
    llm: LlmClient,
    userId: Long,
    modifier: Modifier = Modifier,
) {
    val scope = rememberCoroutineScope()
    var version by remember { mutableIntStateOf(0) }
    var showForm by remember { mutableStateOf(false) }
    var showNl by remember { mutableStateOf(false) }
    var formInitial by remember { mutableStateOf(ReminderFormData()) }
    var pendingPostpone by remember { mutableStateOf<Reminder?>(null) }

    val state by produceState<ScreenState<List<Reminder>>>(
        initialValue = ScreenState.Loading,
        key1 = version,
    ) {
        value = try {
            ScreenState.Data(notifications.listReminders(userId))
        } catch (e: Exception) {
            ScreenState.Error(friendlyMessage(e))
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Button(onClick = { formInitial = ReminderFormData(); showForm = true }) {
                Text("Добавить")
            }
            Button(onClick = { showNl = true }) {
                Text("Из текста")
            }
        }

        when (val s = state) {
            is ScreenState.Loading -> LoadingView(modifier = Modifier.weight(1f))
            is ScreenState.Error -> ErrorView(s.message, onRetry = { version++ }, modifier = Modifier.weight(1f))
            is ScreenState.Data -> {
                if (s.data.isEmpty()) {
                    EmptyView("Напоминаний пока нет", modifier = Modifier.weight(1f))
                } else {
                    LazyColumn(
                        modifier = Modifier.weight(1f),
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(s.data, key = { it.id }) { reminder ->
                            ReminderCard(
                                reminder = reminder,
                                onDelete = {
                                    scope.launch {
                                        notifications.deleteReminder(reminder.id, userId)
                                        version++
                                    }
                                },
                                onPostpone = { pendingPostpone = reminder },
                            )
                        }
                    }
                }
            }
        }
    }

    if (showForm) {
        ReminderFormDialog(
            tzOffset = Dates.currentTzOffsetHours(),
            initial = formInitial,
            onDismiss = { showForm = false },
            onSave = { form ->
                val (params, err) = buildScheduleParams(form, Dates.currentTzOffsetHours())
                if (err != null || params == null) return@ReminderFormDialog
                scope.launch {
                    runCatching {
                        notifications.createReminder(
                            userId = userId,
                            title = form.title.ifBlank { "Напоминание" },
                            scheduleType = form.scheduleType,
                            params = params,
                            createTask = form.createTask,
                        )
                    }
                    showForm = false
                    version++
                }
            },
        )
    }

    if (showNl) {
        NlReminderDialog(
            llm = llm,
            onDismiss = { showNl = false },
            onParsed = { result ->
                showNl = false
                formInitial = result.toForm()
                showForm = true
            },
        )
    }

    pendingPostpone?.let { reminder ->
        PostponeReminderDialog(
            reminder = reminder,
            onDismiss = { pendingPostpone = null },
            onPostpone = { minutes ->
                scope.launch {
                    notifications.postponeReminder(reminder.id, userId, minutes)
                    pendingPostpone = null
                    version++
                }
            },
        )
    }
}

@Composable
private fun ReminderCard(
    reminder: Reminder,
    onDelete: () -> Unit,
    onPostpone: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = reminder.title,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                IconButton(onClick = onDelete) {
                    Icon(Icons.Filled.Delete, contentDescription = "Удалить")
                }
            }
            Text(
                text = scheduleLabel(reminder.scheduleType),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            val next = reminder.nextFireAt
            if (next != null) {
                Text(
                    text = "Следующий раз: ${Dates.formatTimestamp(next.seconds, next.nanos)}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (reminder.createTask) {
                Text(
                    text = "Создаёт задачу в дневнике",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.tertiary,
                )
            }
            Spacer(modifier = Modifier.height(4.dp))
            TextButton(onClick = onPostpone) {
                Text("Перенести")
            }
        }
    }
}

@Composable
private fun NlReminderDialog(
    llm: LlmClient,
    onDismiss: () -> Unit,
    onParsed: (com.example.notes.data.llm.LLMReminderResult) -> Unit,
) {
    var text by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text("Напоминание из текста") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    label = { Text("Опишите напоминание") },
                    placeholder = { Text("например: завтра в 9 утра позвонить маме") },
                    modifier = Modifier.fillMaxWidth(),
                )
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
                enabled = text.isNotBlank() && !busy,
                onClick = {
                    busy = true
                    error = null
                    scope.launch {
                        val today = LocalDate.now()
                        val res = runCatching {
                            llm.parseReminder(
                                text = text.trim(),
                                currentDateTime = Dates.nowDateTimeString(),
                                today = Dates.toIso(today),
                                tomorrow = Dates.toIso(today.plusDays(1)),
                                dayAfter = Dates.toIso(today.plusDays(2)),
                            )
                        }
                        busy = false
                        res.onSuccess { onParsed(it) }
                            .onFailure { error = friendlyMessage(it) }
                    }
                },
            ) {
                Text(if (busy) "Разбираю…" else "Разобрать")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss, enabled = !busy) {
                Text("Отмена")
            }
        },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PostponeReminderDialog(
    reminder: Reminder,
    onDismiss: () -> Unit,
    onPostpone: (Int) -> Unit,
) {
    var duration by remember { mutableStateOf("") }
    var byMode by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var date by remember { mutableStateOf("") }
    var hour by remember { mutableStateOf("") }
    var minute by remember { mutableStateOf("") }
    var showDatePicker by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Перенести: ${reminder.title}") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(
                        selected = byMode,
                        onClick = { byMode = true },
                        label = { Text("По длительности") },
                    )
                    FilterChip(
                        selected = !byMode,
                        onClick = { byMode = false },
                        label = { Text("На дату и время") },
                    )
                }

                if (byMode) {
                    OutlinedTextField(
                        value = duration,
                        onValueChange = { duration = it },
                        label = { Text("Длительность") },
                        placeholder = { Text("30m, 2h30m, 1d12h, 1w, 1M") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                } else {
                    OutlinedTextField(
                        value = date,
                        onValueChange = { date = it },
                        label = { Text("Дата (ГГГГ-ММ-ДД)") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    TextButton(onClick = { showDatePicker = true }) {
                        Text("Выбрать дату")
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedTextField(
                            value = hour,
                            onValueChange = { hour = it },
                            label = { Text("Час") },
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            singleLine = true,
                            modifier = Modifier.weight(1f),
                        )
                        OutlinedTextField(
                            value = minute,
                            onValueChange = { minute = it },
                            label = { Text("Минуты") },
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                            singleLine = true,
                            modifier = Modifier.weight(1f),
                        )
                    }
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
                    error = null
                    if (byMode) {
                        val (minutes, err) = parseDuration(duration)
                        if (err != null) {
                            error = err
                        } else {
                            onPostpone(minutes)
                        }
                    } else {
                        val h = hour.trim().toIntOrNull()
                        val m = minute.trim().toIntOrNull()
                        if (date.isBlank()) {
                            error = "Укажите дату"
                            return@TextButton
                        }
                        if (h == null || m == null || h !in 0..23 || m !in 0..59) {
                            error = "Введите время в формате ЧЧ:ММ"
                            return@TextButton
                        }
                        val target = runCatching {
                            LocalDate.parse(date, DateTimeFormatter.ISO_LOCAL_DATE).atTime(h, m)
                        }.getOrNull()
                        if (target == null) {
                            error = "Некорректная дата"
                            return@TextButton
                        }
                        val minutes = ChronoUnit.MINUTES.between(LocalDateTime.now(), target).toInt()
                        if (minutes < 1) {
                            error = "Выбранное время уже прошло"
                            return@TextButton
                        }
                        onPostpone(minutes)
                    }
                },
            ) {
                Text("Перенести")
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
        androidx.compose.material3.DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(
                    onClick = {
                        dateState.selectedDateMillis?.let { ms ->
                            date = java.time.Instant.ofEpochMilli(ms)
                                .atZone(java.time.ZoneOffset.UTC)
                                .toLocalDate()
                                .format(DateTimeFormatter.ISO_LOCAL_DATE)
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
            androidx.compose.material3.DatePicker(state = dateState)
        }
    }
}

/**
 * Parses a human-readable duration into total minutes. Mirrors the Go
 * frontends' parseDuration (m/h/d/w/M units or a bare integer of minutes).
 */
fun parseDuration(s: String): Pair<Int, String?> {
    val clean = s.trim()
    if (clean.isEmpty()) {
        return 0 to "Неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M"
    }
    clean.toIntOrNull()?.let { n ->
        if (n <= 0) return 0 to "Введите положительное значение"
        return n to null
    }

    val compact = clean.replace(" ", "")
    val vals = mutableMapOf<Char, Int>()
    var i = 0
    while (i < compact.length) {
        var j = i
        while (j < compact.length && compact[j].isDigit()) j++
        if (j == i) return 0 to "Неверный формат — ожидается число перед единицей. Примеры: 30m, 2h30m, 1d12h"
        if (j >= compact.length) return 0 to "Неверный формат — укажите единицу после числа. Доступные: m h d w M"
        val n = compact.substring(i, j).toInt()
        val unit = compact[j]
        if (unit !in "mhdwM") {
            return 0 to "Неизвестная единица \"$unit\". Доступные: m (минуты), h (часы), d (дни), w (недели), M (месяцы)"
        }
        if (vals.containsKey(unit)) return 0 to "Единица \"$unit\" указана дважды"
        vals[unit] = n
        i = j + 1
    }

    if (vals.isEmpty()) return 0 to "Неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M"
    if (vals['m'] != null && vals['m']!! >= 60) return 0 to "${vals['m']}m — это больше часа; используйте h/d/w"
    if (vals['h'] != null && vals['h']!! >= 24) return 0 to "${vals['h']}h — это больше суток; используйте d/w"
    if (vals['d'] != null && vals['d']!! >= 7) return 0 to "${vals['d']}d — это больше недели; используйте w"

    val total = (vals['m'] ?: 0) + (vals['h'] ?: 0) * 60 + (vals['d'] ?: 0) * 1440 +
        (vals['w'] ?: 0) * 10080 + (vals['M'] ?: 0) * 43200
    if (total <= 0) return 0 to "Введите положительное значение"
    return total to null
}