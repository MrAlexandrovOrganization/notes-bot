package com.example.notes.data.grpc

import io.grpc.ManagedChannel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import search.Search
import search.Search.AskRequest
import search.Search.AskResponse
import search.Search.GetNoteRequest
import search.Search.Hit
import search.Search.Note
import search.Search.SearchRequest
import search.SearchServiceGrpc

/**
 * Blocking gRPC client for the search service (product-level FindNotes,
 * GetNote, RAG AskNotes). Mirrors the Go frontends' SearchClient.
 */
class SearchClient(private val channel: ManagedChannel) {

    private val stub: SearchServiceGrpc.SearchServiceBlockingStub =
        SearchServiceGrpc.newBlockingStub(channel)

    suspend fun findNotes(query: String, limit: Int): List<Hit> = withContext(Dispatchers.IO) {
        stub.findNotes(
            SearchRequest.newBuilder()
                .setQuery(query)
                .setLimit(limit)
                .build(),
        ).hitsList
    }

    suspend fun getNoteById(id: Long): Note? = withContext(Dispatchers.IO) {
        val note = stub.getNote(
            GetNoteRequest.newBuilder()
                .setId(id)
                .build(),
        )
        if (note.id == 0L && note.relpath.isEmpty()) null else note
    }

    suspend fun askNotes(question: String, currentDatetime: String): AskResponse =
        withContext(Dispatchers.IO) {
            stub.askNotes(
                AskRequest.newBuilder()
                    .setQuestion(question)
                    .setCurrentDatetime(currentDatetime)
                    .build(),
            )
        }
}