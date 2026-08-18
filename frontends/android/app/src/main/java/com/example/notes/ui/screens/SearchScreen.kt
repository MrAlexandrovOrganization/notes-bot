package com.example.notes.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.unit.dp
import com.example.notes.data.grpc.CoreClient
import com.example.notes.data.grpc.SearchClient
import com.example.notes.ui.components.EmptyView
import com.example.notes.ui.components.ErrorView
import com.example.notes.ui.components.LoadingView
import com.example.notes.ui.components.ScreenState
import com.example.notes.util.friendlyMessage
import kotlinx.coroutines.launch
import search.Search.Hit
import search.Search.Note

private const val SEARCH_LIMIT = 25

@Composable
fun SearchScreen(
    search: SearchClient,
    core: CoreClient,
    modifier: Modifier = Modifier,
) {
    var query by remember { mutableStateOf("") }
    var submitted by remember { mutableStateOf(false) }
    var version by remember { mutableIntStateOf(0) }
    var openNoteId by remember { mutableStateOf<Long?>(null) }

    if (openNoteId != null) {
        SearchNoteView(
            search = search,
            core = core,
            noteId = openNoteId!!,
            onBack = { openNoteId = null },
            modifier = modifier,
        )
        return
    }

    val state by produceState<ScreenState<List<Hit>>>(
        initialValue = ScreenState.Data(emptyList()),
        key1 = submitted,
        key2 = query,
        key3 = version,
    ) {
        if (!submitted || query.isBlank()) {
            value = ScreenState.Data(emptyList())
        } else {
            value = try {
                ScreenState.Data(search.findNotes(query.trim(), SEARCH_LIMIT))
            } catch (e: Exception) {
                ScreenState.Error(friendlyMessage(e))
            }
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                label = { Text("Поиск") },
                singleLine = true,
                modifier = Modifier.weight(1f),
            )
            TextButton(
                enabled = query.isNotBlank(),
                onClick = {
                    submitted = true
                    version++
                },
            ) {
                Text("Найти")
            }
        }

        when (val s = state) {
            is ScreenState.Loading -> LoadingView(modifier = Modifier.weight(1f))
            is ScreenState.Error -> ErrorView(s.message, modifier = Modifier.weight(1f))
            is ScreenState.Data -> {
                if (submitted && s.data.isEmpty()) {
                    EmptyView("Ничего не найдено", modifier = Modifier.weight(1f))
                } else if (!submitted) {
                    EmptyView("Введите запрос для поиска по заметкам", modifier = Modifier.weight(1f))
                } else {
                    LazyColumn(
                        modifier = Modifier.weight(1f),
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(s.data, key = { it.noteId }) { hit ->
                            HitCard(hit = hit, onClick = { openNoteId = hit.noteId })
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun HitCard(hit: Hit, onClick: () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
    ) {
        Column(modifier = Modifier.padding(12.dp)) {
            Text(
                text = hit.name.ifBlank { hit.relpath },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
            hit.relpath.takeIf { it != hit.name }?.let {
                Text(
                    text = it,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (hit.snippet.isNotBlank()) {
                Text(
                    text = hit.snippet,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 3,
                )
            }
        }
    }
}

@Composable
private fun SearchNoteView(
    search: SearchClient,
    core: CoreClient,
    noteId: Long,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var version by remember { mutableIntStateOf(0) }
    var appendText by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    val state by produceState<ScreenState<Note>>(
        initialValue = ScreenState.Loading,
        key1 = noteId,
        key2 = version,
    ) {
        value = try {
            val note = search.getNoteById(noteId)
            if (note == null) ScreenState.Error("Заметка не найдена") else ScreenState.Data(note)
        } catch (e: Exception) {
            ScreenState.Error(friendlyMessage(e))
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        val currentState = state
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.ArrowBack, contentDescription = "Назад")
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (currentState is ScreenState.Data) currentState.data.name else "",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                )
                if (currentState is ScreenState.Data && currentState.data.relpath.isNotBlank()) {
                    Text(
                        text = currentState.data.relpath,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }

        when (val s = state) {
            is ScreenState.Loading -> LoadingView(modifier = Modifier.weight(1f))
            is ScreenState.Error -> ErrorView(s.message, modifier = Modifier.weight(1f))
            is ScreenState.Data -> {
                LazyColumn(
                    modifier = Modifier.weight(1f),
                    contentPadding = PaddingValues(12.dp),
                ) {
                    item {
                        Text(
                            text = s.data.content.ifBlank { "Пусто" },
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                    item {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 12.dp),
                            verticalAlignment = Alignment.Bottom,
                        ) {
                            OutlinedTextField(
                                value = appendText,
                                onValueChange = { appendText = it },
                                label = { Text("Дописать") },
                                modifier = Modifier.weight(1f),
                            )
                            TextButton(
                                enabled = appendText.isNotBlank(),
                                onClick = {
                                    scope.launch {
                                        core.appendToNoteByPath(s.data.relpath, appendText.trim())
                                        appendText = ""
                                        version++
                                    }
                                },
                            ) {
                                Text("Дописать")
                            }
                        }
                    }
                }
            }
        }
    }
}