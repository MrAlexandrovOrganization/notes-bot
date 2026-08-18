package com.example.notes.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.example.notes.data.grpc.CoreClient
import com.example.notes.ui.components.ErrorView
import com.example.notes.ui.components.LoadingView
import com.example.notes.ui.components.ScreenState
import com.example.notes.util.Dates
import com.example.notes.util.friendlyMessage
import java.time.LocalDate

private val WEEKDAY_LABELS = listOf("Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс")
private val MONTH_NAMES = listOf(
    "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
    "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
)

private data class CalendarData(
    val year: Int,
    val month: Int,
    val existing: Set<String>,
    val today: String,
)

@Composable
fun CalendarScreen(
    core: CoreClient,
    onDayClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val now = remember { LocalDate.now() }
    var year by remember { mutableIntStateOf(now.year) }
    var month by remember { mutableIntStateOf(now.monthValue) }
    var version by remember { mutableIntStateOf(0) }

    val state by produceState<ScreenState<CalendarData>>(
        initialValue = ScreenState.Loading,
        key1 = version,
    ) {
        value = try {
            val existing = core.getExistingDates().toSet()
            val today = core.getTodayDate()
            ScreenState.Data(CalendarData(year, month, existing, today))
        } catch (e: Exception) {
            ScreenState.Error(friendlyMessage(e))
        }
    }

    when (val s = state) {
        is ScreenState.Loading -> LoadingView(modifier = modifier.fillMaxSize())
        is ScreenState.Error -> ErrorView(s.message, onRetry = { version++ }, modifier = modifier)
        is ScreenState.Data -> {
            val weeks = buildWeeks(year, month, s.data.today, s.data.existing)
            Column(modifier = modifier.fillMaxSize()) {
                CalendarHeader(
                    year = year,
                    month = month,
                    onPrev = {
                        if (month == 1) {
                            month = 12
                            year--
                        } else {
                            month--
                        }
                    },
                    onNext = {
                        if (month == 12) {
                            month = 1
                            year++
                        } else {
                            month++
                        }
                    },
                    onToday = {
                        year = now.year
                        month = now.monthValue
                        version++
                    },
                )
                WeekdayHeader()
                weeks.forEach { week ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .weight(1f, fill = false),
                    ) {
                        week.forEach { cell ->
                            CalendarCell(
                                cell = cell,
                                onDayClick = onDayClick,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun CalendarHeader(
    year: Int,
    month: Int,
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
            Icon(Icons.Filled.KeyboardArrowLeft, contentDescription = "Предыдущий месяц")
        }
        Column(
            modifier = Modifier.weight(1f),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = "${MONTH_NAMES[month - 1]} $year",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
        }
        IconButton(onClick = onNext) {
            Icon(Icons.Filled.KeyboardArrowRight, contentDescription = "Следующий месяц")
        }
        TextButton(onClick = onToday) { Text("Сегодня") }
    }
}

@Composable
private fun WeekdayHeader() {
    Row(modifier = Modifier.fillMaxWidth()) {
        WEEKDAY_LABELS.forEach { label ->
            Box(modifier = Modifier.weight(1f), contentAlignment = Alignment.Center) {
                Text(
                    text = label,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

private data class CalendarCellData(
    val day: Int,
    val date: String,
    val hasNote: Boolean,
    val isToday: Boolean,
)

@Composable
private fun CalendarCell(
    cell: CalendarCellData?,
    onDayClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .aspectRatio(1f)
            .padding(2.dp),
        contentAlignment = Alignment.Center,
    ) {
        if (cell != null) {
            val bg = when {
                cell.isToday -> MaterialTheme.colorScheme.primary
                cell.hasNote -> MaterialTheme.colorScheme.surfaceVariant
                else -> MaterialTheme.colorScheme.surface
            }
            val fg = when {
                cell.isToday -> MaterialTheme.colorScheme.onPrimary
                else -> MaterialTheme.colorScheme.onSurface
            }
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(bg, CircleShape)
                    .clickable { onDayClick(cell.date) },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = cell.day.toString(),
                    style = MaterialTheme.typography.bodyMedium,
                    color = fg,
                    fontWeight = if (cell.isToday) FontWeight.Bold else FontWeight.Normal,
                )
            }
        }
    }
}

/** Builds Monday-start weeks for the month, matching the web/telegram convention. */
private fun buildWeeks(
    year: Int,
    month: Int,
    today: String,
    existing: Set<String>,
): List<List<CalendarCellData?>> {
    val firstDay = LocalDate.of(year, month, 1)
    val startOffset = (firstDay.dayOfWeek.value + 6) % 7 // Monday = 0
    val daysInMonth = firstDay.lengthOfMonth()

    val weeks = mutableListOf<List<CalendarCellData?>>()
    var day = 1
    var row = 0
    while (day <= daysInMonth) {
        val week = mutableListOf<CalendarCellData?>()
        for (col in 0 until 7) {
            if ((row == 0 && col < startOffset) || day > daysInMonth) {
                week.add(null)
                continue
            }
            val date = LocalDate.of(year, month, day)
            val dateStr = Dates.formatNoteDate(date)
            week.add(
                CalendarCellData(
                    day = day,
                    date = dateStr,
                    hasNote = dateStr in existing,
                    isToday = dateStr == today,
                ),
            )
            day++
        }
        weeks.add(week)
        row++
    }
    return weeks
}