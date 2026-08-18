package com.example.notes.data.grpc

import com.google.protobuf.Empty
import io.grpc.ManagedChannel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import notes.Notes
import notes.Notes.DateRequest
import notes.Notes.DirEntry
import notes.Notes.Task
import notes.NotesServiceGrpc

/**
 * Blocking gRPC client for the notes core service. Methods mirror the Go
 * frontends' CoreClient (notes CRUD, tasks, ratings, directory browsing).
 * All calls run on Dispatchers.IO.
 */
class CoreClient(private val channel: ManagedChannel) {

    private val stub: NotesServiceGrpc.NotesServiceBlockingStub =
        NotesServiceGrpc.newBlockingStub(channel)

    suspend fun getTodayDate(): String = withContext(Dispatchers.IO) {
        stub.getTodayDate(Empty.getDefaultInstance()).date
    }

    suspend fun getExistingDates(): List<String> = withContext(Dispatchers.IO) {
        stub.getExistingDates(Empty.getDefaultInstance()).datesList
    }

    suspend fun ensureNote(date: String) = withContext(Dispatchers.IO) {
        stub.ensureNote(DateRequest.newBuilder().setDate(date).build())
    }

    suspend fun getNote(date: String): String = withContext(Dispatchers.IO) {
        stub.getNote(DateRequest.newBuilder().setDate(date).build()).content
    }

    suspend fun getRating(date: String): Pair<Boolean, Int> = withContext(Dispatchers.IO) {
        val r = stub.getRating(DateRequest.newBuilder().setDate(date).build())
        r.hasRating to r.rating
    }

    suspend fun updateRating(date: String, rating: Int) = withContext(Dispatchers.IO) {
        stub.updateRating(
            Notes.UpdateRatingRequest.newBuilder()
                .setDate(date)
                .setRating(rating)
                .build(),
        )
    }

    suspend fun getTasks(date: String): List<Task> = withContext(Dispatchers.IO) {
        stub.getTasks(DateRequest.newBuilder().setDate(date).build()).tasksList
    }

    suspend fun toggleTask(date: String, taskIndex: Int) = withContext(Dispatchers.IO) {
        stub.toggleTask(
            Notes.ToggleTaskRequest.newBuilder()
                .setDate(date)
                .setTaskIndex(taskIndex)
                .build(),
        )
    }

    suspend fun addTask(date: String, text: String) = withContext(Dispatchers.IO) {
        stub.addTask(
            Notes.AddTaskRequest.newBuilder()
                .setDate(date)
                .setTaskText(text)
                .build(),
        )
    }

    suspend fun appendToNote(date: String, text: String) = withContext(Dispatchers.IO) {
        stub.appendToNote(
            Notes.AppendRequest.newBuilder()
                .setDate(date)
                .setText(text)
                .build(),
        )
    }

    suspend fun appendToNoteByPath(relpath: String, text: String) = withContext(Dispatchers.IO) {
        stub.appendToNoteByPath(
            Notes.AppendByPathRequest.newBuilder()
                .setRelpath(relpath)
                .setText(text)
                .build(),
        )
    }

    suspend fun listDirectory(relpath: String): List<DirEntry> = withContext(Dispatchers.IO) {
        stub.listDirectory(
            Notes.ListDirectoryRequest.newBuilder().setRelpath(relpath).build(),
        ).entriesList
    }

    suspend fun getNoteByPath(relpath: String): String = withContext(Dispatchers.IO) {
        stub.getNoteByPath(
            Notes.GetNoteByPathRequest.newBuilder().setRelpath(relpath).build(),
        ).content
    }
}