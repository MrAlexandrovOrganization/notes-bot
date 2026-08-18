package com.example.notes.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import com.example.notes.data.grpc.CoreClient
import com.example.notes.ui.components.ErrorView
import com.example.notes.ui.components.LoadingView
import com.example.notes.ui.components.ScreenState
import com.example.notes.util.Dates
import com.example.notes.util.friendlyMessage
import kotlinx.coroutines.launch
import notes.Notes.Task

private data class DayData(
    val date: String,
    val content: String,
    val hasRating: Boolean,
    val rating: Int,
    val tasks: List<Task>,
)

@Composable
fun DayScreen(
    core: CoreClient,
    date: String,
    onDateChange: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var version by remember { mutableIntStateOf(0) }
    val isToday = date.isBlank()
    val state by produceState<ScreenState<DayData>>(
        initialValue = ScreenState.Loading,
        key1 = date,
        key2 = version,
    ) {
        value = try {
            val resolved = if (date.isBlank()) core.getTodayDate() else date
            core.ensureNote(resolved)
            val content = core.getNote(resolved)
            val (hasRating, rating) = core.getRating(resolved)
            val tasks = core.getTasks(resolved)
            ScreenState.Data(DayData(resolved, content, hasRating, rating, tasks))
        } catch (e: Exception) {
            ScreenState.Error(friendlyMessage(e))
        }
    }

    when (val s = state) {
        is ScreenState.Loading -> LoadingView(modifier = modifier.fillMaxSize())
        is ScreenState.Error -> ErrorView(s.message, modifier = modifier)
        is ScreenState.Data -> DayContent(
            core = core,
            data = s.data,
            isToday = isToday,
            onReload = { version++ },
            onDateChange = onDateChange,
            modifier = modifier,
        )
    }
}

@Composable
private fun DayContent(
    core: CoreClient,
    data: DayData,
    isToday: Boolean,
    onReload: () -> Unit,
    onDateChange: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    var newTask by remember { mutableStateOf("") }
    var appendText by remember { mutableStateOf("") }
    var rating by remember(data.date) { mutableStateOf(data.rating.toFloat()) }

    LazyColumn(modifier = modifier.fillMaxSize()) {
        item {
            DateNavigator(
                date = data.date,
                isToday = isToday,
                onPrev = { onDateChange(Dates.formatNoteDate(Dates.parseNoteDate(data.date).minusDays(1))) },
                onNext = { onDateChange(Dates.formatNoteDate(Dates.parseNoteDate(data.date).plusDays(1))) },
                onToday = { onDateChange("") },
            )
        }

        item {
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                colors = CardDefaults.cardColors(
                    containerColor = MaterialTheme.colorScheme.surfaceVariant,
                ),
            ) {
                Column(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            text = "Оценка",
                            style = MaterialTheme.typography.labelLarge,
                            modifier = Modifier.weight(1f),
                        )
                        Text(
                            text = "${rating.toInt()}/10",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold,
                        )
                    }
                    Slider(
                        value = rating,
                        onValueChange = { rating = it },
                        valueRange = 0f..10f,
                        steps = 10,
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.End,
                    ) {
                        TextButton(
                            onClick = {
                                scope.launch {
                                    busy = true
                                    runCatching { core.updateRating(data.date, rating.toInt()) }
                                    busy = false
                                    onReload()
                                }
                            },
                            enabled = !busy,
                        ) {
                            Text("Сохранить")
                        }
                    }
                }
            }
        }

        item {
            Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
                Text(
                    text = "Задачи",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                )
                if (data.tasks.isEmpty()) {
                    Text(
                        text = "Задач нет",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    data.tasks.forEach { task ->
                        TaskRow(
                            task = task,
                            onToggle = {
                                scope.launch {
                                    core.toggleTask(data.date, task.index)
                                    onReload()
                                }
                            },
                        )
                    }
                }
                Spacer(modifier = Modifier.height(8.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    OutlinedTextField(
                        value = newTask,
                        onValueChange = { newTask = it },
                        label = { Text("Новая задача") },
                        modifier = Modifier.weight(1f),
                        singleLine = true,
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    IconButton(
                        onClick = {
                            if (newTask.isBlank()) return@IconButton
                            scope.launch {
                                core.addTask(data.date, newTask.trim())
                                newTask = ""
                                onReload()
                            }
                        },
                    ) {
                        Icon(Icons.Filled.Check, contentDescription = "Добавить задачу")
                    }
                }
            }
        }

        item {
            Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
                Text(
                    text = "Заметка",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                )
                if (data.content.isBlank()) {
                    Text(
                        text = "Пусто",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    Text(
                        text = data.content,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
        }

        item {
            Column(modifier = Modifier.padding(12.dp)) {
                OutlinedTextField(
                    value = appendText,
                    onValueChange = { appendText = it },
                    label = { Text("Дописать в заметку") },
                    modifier = Modifier.fillMaxWidth(),
                    minLines = 2,
                )
                Spacer(modifier = Modifier.height(8.dp))
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                    TextButton(
                        enabled = appendText.isNotBlank() && !busy,
                        onClick = {
                            scope.launch {
                                busy = true
                                runCatching { core.appendToNote(data.date, appendText.trim()) }
                                appendText = ""
                                busy = false
                                onReload()
                            }
                        },
                    ) {
                        Text("Дописать")
                    }
                }
            }
        }
        item { Spacer(modifier = Modifier.height(24.dp)) }
    }
}

@Composable
private fun DateNavigator(
    date: String,
    isToday: Boolean,
    onPrev: () -> Unit,
    onNext: () -> Unit,
    onToday: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 4.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onPrev) {
            Icon(Icons.Filled.KeyboardArrowLeft, contentDescription = "Предыдущий день")
        }
        Column(
            modifier = Modifier.weight(1f),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = date,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
            if (isToday) {
                Text(
                    text = "сегодня",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        IconButton(onClick = onNext) {
            Icon(Icons.Filled.KeyboardArrowRight, contentDescription = "Следующий день")
        }
        TextButton(onClick = onToday) { Text("Сегодня") }
    }
}

@Composable
private fun TaskRow(task: Task, onToggle: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onToggle)
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = task.completed, onCheckedChange = { onToggle() })
        Text(
            text = task.text,
            style = MaterialTheme.typography.bodyMedium,
            textDecoration = if (task.completed) TextDecoration.LineThrough else TextDecoration.None,
            color = if (task.completed) {
                MaterialTheme.colorScheme.onSurfaceVariant
            } else {
                MaterialTheme.colorScheme.onSurface
            },
        )
    }
}