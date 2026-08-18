package com.example.notes.util

import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Calendar
import java.util.Locale

/**
 * Date helpers matching the backend conventions:
 *  - note dates are "DD-Mmm-YYYY" (e.g. "09-Nov-2025")
 *  - reminder "once" dates are "YYYY-MM-DD"
 *  - AskNotes wants current datetime as "YYYY-MM-DD HH:MM"
 */
object Dates {

    private val noteFormat = DateTimeFormatter.ofPattern("dd-MMM-yyyy", Locale.ENGLISH)
    private val iso = DateTimeFormatter.ISO_LOCAL_DATE
    private val datetimeFormat = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm")

    fun formatNoteDate(d: LocalDate): String = d.format(noteFormat)

    fun parseNoteDate(s: String): LocalDate = LocalDate.parse(s, noteFormat)

    fun toIso(d: LocalDate): String = d.format(iso)

    fun fromIso(s: String): LocalDate = LocalDate.parse(s, iso)

    fun nowDateTimeString(): String =
        java.time.LocalDateTime.now().format(datetimeFormat)

    /** Current UTC offset of the device in whole hours (backend uses int tz_offset). */
    fun currentTzOffsetHours(): Int {
        val cal = Calendar.getInstance()
        return cal.timeZone.getOffset(cal.timeInMillis) / 3_600_000
    }

    /** Formats a protobuf Timestamp as "DD.MM.YYYY HH:MM" in the device timezone. */
    fun formatTimestamp(seconds: Long, nanos: Int): String {
        val instant = java.time.Instant.ofEpochSecond(seconds, nanos.toLong())
        return java.time.LocalDateTime
            .ofInstant(instant, ZoneOffset.systemDefault())
            .format(DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm"))
    }
}