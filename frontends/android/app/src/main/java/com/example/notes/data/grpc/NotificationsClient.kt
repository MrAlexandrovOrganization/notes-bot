package com.example.notes.data.grpc

import io.grpc.ManagedChannel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import notifications.Notifications
import notifications.Notifications.CreateReminderRequest
import notifications.Notifications.DeleteReminderRequest
import notifications.Notifications.ListRemindersRequest
import notifications.Notifications.PostponeReminderRequest
import notifications.Notifications.Reminder
import notifications.Notifications.ScheduleParams
import notifications.NotificationsServiceGrpc

/**
 * Blocking gRPC client for the notifications service. Reminders are scoped by
 * user_id — the app must send the same user id (ROOT_ID from the Telegram/web
 * frontends) to see the same reminders everywhere.
 */
class NotificationsClient(private val channel: ManagedChannel) {

    private val stub: NotificationsServiceGrpc.NotificationsServiceBlockingStub =
        NotificationsServiceGrpc.newBlockingStub(channel)

    suspend fun listReminders(userId: Long): List<Reminder> = withContext(Dispatchers.IO) {
        stub.listReminders(ListRemindersRequest.newBuilder().setUserId(userId).build()).remindersList
    }

    suspend fun createReminder(
        userId: Long,
        title: String,
        scheduleType: String,
        params: ScheduleParams,
        createTask: Boolean,
    ) = withContext(Dispatchers.IO) {
        stub.createReminder(
            CreateReminderRequest.newBuilder()
                .setUserId(userId)
                .setTitle(title)
                .setScheduleType(scheduleType)
                .setScheduleParams(params)
                .setCreateTask(createTask)
                .build(),
        )
    }

    suspend fun deleteReminder(id: Long, userId: Long) = withContext(Dispatchers.IO) {
        stub.deleteReminder(DeleteReminderRequest.newBuilder().setReminderId(id).setUserId(userId).build())
    }

    suspend fun postponeReminder(id: Long, userId: Long, minutes: Int) = withContext(Dispatchers.IO) {
        stub.postponeReminder(
            PostponeReminderRequest.newBuilder()
                .setReminderId(id)
                .setUserId(userId)
                .setPostponeMinutes(minutes)
                .build(),
        )
    }
}