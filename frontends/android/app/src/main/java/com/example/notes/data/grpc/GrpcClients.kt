package com.example.notes.data.grpc

import android.content.Context
import com.example.notes.R
import com.example.notes.data.llm.LlmClient
import io.grpc.ManagedChannel
import io.grpc.okhttp.OkHttpChannelBuilder

/**
 * Owns the gRPC channels to the three backend services (core, notifications,
 * search) plus the HTTP client to the local Ollama LLM. Mirrors the client
 * wiring of the Go frontends: core=50051, notifications=50052, search=50054.
 *
 * The services are plaintext gRPC (no TLS), so channels are built with
 * OkHttpChannelBuilder.forAddress() without an sslSocketFactory — the same way
 * the web/telegram frontends talk to them inside the Docker network.
 */
class GrpcClients(context: Context) {

    val core: CoreClient
    val notifications: NotificationsClient
    val search: SearchClient
    val llm: LlmClient

    private val coreChannel: ManagedChannel
    private val notificationsChannel: ManagedChannel
    private val searchChannel: ManagedChannel

    init {
        val res = context.resources
        val coreChan = plainChannel(
            res.getString(R.string.core_host),
            res.getInteger(R.integer.core_port),
        )
        val notificationsChan = plainChannel(
            res.getString(R.string.notifications_host),
            res.getInteger(R.integer.notifications_port),
        )
        val searchChan = plainChannel(
            res.getString(R.string.search_host),
            res.getInteger(R.integer.search_port),
        )
        coreChannel = coreChan
        notificationsChannel = notificationsChan
        searchChannel = searchChan

        core = CoreClient(coreChan)
        notifications = NotificationsClient(notificationsChan)
        search = SearchClient(searchChan)
        llm = LlmClient(
            host = res.getString(R.string.llm_host),
            port = res.getInteger(R.integer.llm_port),
            model = res.getString(R.string.llm_model),
        )
    }

    fun shutdown() {
        coreChannel.shutdown()
        notificationsChannel.shutdown()
        searchChannel.shutdown()
    }

    private fun plainChannel(host: String, port: Int): ManagedChannel =
        OkHttpChannelBuilder.forAddress(host, port).build()
}