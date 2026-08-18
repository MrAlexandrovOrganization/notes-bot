package com.example.notes.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.KeyboardArrowUp
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
import com.example.notes.ui.components.EmptyView
import com.example.notes.ui.components.ErrorView
import com.example.notes.ui.components.LoadingView
import com.example.notes.ui.components.ScreenState
import com.example.notes.util.friendlyMessage
import kotlinx.coroutines.launch
import notes.Notes.DirEntry

@Composable
fun BrowseScreen(
    core: CoreClient,
    modifier: Modifier = Modifier,
) {
    var path by remember { mutableStateOf("") }
    var version by remember { mutableIntStateOf(0) }
    var openFile by remember { mutableStateOf<String?>(null) }

    val currentFile = openFile
    if (currentFile != null) {
        BrowseFileView(
            core = core,
            relpath = currentFile,
            onBack = { openFile = null },
            modifier = modifier,
        )
        return
    }

    val state by produceState<ScreenState<List<DirEntry>>>(
        initialValue = ScreenState.Loading,
        key1 = path,
        key2 = version,
    ) {
        value = try {
            ScreenState.Data(core.listDirectory(path))
        } catch (e: Exception) {
            ScreenState.Error(friendlyMessage(e))
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 4.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(
                onClick = {
                    val idx = path.lastIndexOf('/')
                    path = if (idx <= 0) "" else path.substring(0, idx)
                    version++
                },
                enabled = path.isNotEmpty(),
            ) {
                Icon(Icons.Filled.KeyboardArrowUp, contentDescription = "Вверх")
            }
            Text(
                text = path.ifEmpty { "/" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f),
            )
        }

        when (val s = state) {
            is ScreenState.Loading -> LoadingView(modifier = Modifier.weight(1f))
            is ScreenState.Error -> ErrorView(s.message, onRetry = { version++ }, modifier = Modifier.weight(1f))
            is ScreenState.Data -> {
                if (s.data.isEmpty()) {
                    EmptyView("Пустая папка", modifier = Modifier.weight(1f))
                } else {
                    LazyColumn(
                        modifier = Modifier.weight(1f),
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        items(s.data, key = { it.relpath }) { entry ->
                            BrowseEntryRow(
                                entry = entry,
                                onClick = {
                                    if (entry.isDir) {
                                        path = entry.relpath
                                        version++
                                    } else {
                                        openFile = entry.relpath
                                    }
                                },
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun BrowseEntryRow(entry: DirEntry, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 4.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (entry.isDir) {
            Icon(
                Icons.Filled.Folder,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
            )
        } else {
            Icon(
                Icons.Filled.ArrowBack,
                contentDescription = null,
                modifier = Modifier.padding(horizontal = 2.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Spacer(modifier = Modifier.padding(4.dp))
        Text(
            text = entry.name,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = if (entry.isDir) FontWeight.SemiBold else FontWeight.Normal,
        )
    }
}

@Composable
private fun BrowseFileView(
    core: CoreClient,
    relpath: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var version by remember { mutableIntStateOf(0) }
    var appendText by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    val state by produceState<ScreenState<String>>(
        initialValue = ScreenState.Loading,
        key1 = relpath,
        key2 = version,
    ) {
        value = try {
            ScreenState.Data(core.getNoteByPath(relpath))
        } catch (e: Exception) {
            ScreenState.Error(friendlyMessage(e))
        }
    }

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 4.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.ArrowBack, contentDescription = "Назад")
            }
            Text(
                text = relpath,
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f),
            )
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
                            text = s.data.ifBlank { "Пусто" },
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
                                        core.appendToNoteByPath(relpath, appendText.trim())
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