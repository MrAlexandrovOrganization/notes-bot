package com.example.notes.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.example.notes.data.grpc.SearchClient
import com.example.notes.ui.components.EmptyView
import com.example.notes.ui.components.LoadingView
import com.example.notes.util.Dates
import com.example.notes.util.friendlyMessage
import io.grpc.Status
import io.grpc.StatusRuntimeException
import kotlinx.coroutines.launch

private data class AskResult(
    val answer: String,
    val sources: List<String>,
)

@Composable
fun AskScreen(
    search: SearchClient,
    modifier: Modifier = Modifier,
) {
    val scope = rememberCoroutineScope()
    var question by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<AskResult?>(null) }
    var error by remember { mutableStateOf<String?>(null) }

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = question,
                onValueChange = { question = it },
                label = { Text("Вопрос по вашим заметкам") },
                modifier = Modifier.weight(1f),
            )
            TextButton(
                enabled = question.isNotBlank() && !busy,
                onClick = {
                    busy = true
                    error = null
                    scope.launch {
                        val res = runCatching {
                            search.askNotes(question.trim(), Dates.nowDateTimeString())
                        }
                        busy = false
                        res.onSuccess { resp ->
                            val sources = resp.evidenceList
                                .mapNotNull { it.name.takeIf { n -> n.isNotBlank() } }
                                .distinct()
                            val answer = resp.answer.trim()
                            result = AskResult(
                                answer = answer.ifEmpty { "Не нашёл в заметках." },
                                sources = sources,
                            )
                        }.onFailure {
                            error = when {
                                it is StatusRuntimeException && it.status.code == Status.Code.UNIMPLEMENTED ->
                                    "Агентный поиск выключен на сервере."
                                it is StatusRuntimeException && it.status.code == Status.Code.UNAVAILABLE ->
                                    "Эмбеддер недоступен. Проверьте Ollama."
                                else -> friendlyMessage(it)
                            }
                        }
                    }
                },
            ) {
                Text("Спросить")
            }
        }

        when {
            busy -> LoadingView(modifier = Modifier.weight(1f))
            error != null -> {
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth()
                        .padding(24.dp),
                ) {
                    Text(
                        text = error!!,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
            result == null -> EmptyView(
                "Задайте вопрос — приложение найдёт ответ в ваших заметках.",
                modifier = Modifier.weight(1f),
            )
            else -> {
                val r = result!!
                LazyColumn(
                    modifier = Modifier.weight(1f),
                    contentPadding = PaddingValues(12.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    item {
                        Text(
                            text = r.answer,
                            style = MaterialTheme.typography.bodyLarge,
                        )
                    }
                    if (r.sources.isNotEmpty()) {
                        item {
                            Text(
                                text = "Источники",
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                        items(r.sources.size) { i ->
                            Card(modifier = Modifier.fillMaxWidth()) {
                                Text(
                                    text = r.sources[i],
                                    style = MaterialTheme.typography.bodySmall,
                                    modifier = Modifier.padding(12.dp),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}