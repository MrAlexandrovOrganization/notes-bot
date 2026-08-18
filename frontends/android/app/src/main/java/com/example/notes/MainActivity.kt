package com.example.notes

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import com.example.notes.data.grpc.GrpcClients
import com.example.notes.ui.screens.AskScreen
import com.example.notes.ui.screens.BrowseScreen
import com.example.notes.ui.screens.CalendarScreen
import com.example.notes.ui.screens.DayScreen
import com.example.notes.ui.screens.RemindersScreen
import com.example.notes.ui.screens.SearchScreen
import com.example.notes.ui.theme.NotesTheme

private enum class Tab(val label: String, val icon: ImageVector) {
    Day("День", Icons.Filled.Home),
    Calendar("Календарь", Icons.Filled.DateRange),
    Reminders("Напоминания", Icons.Filled.Notifications),
    Search("Поиск", Icons.Filled.Search),
    Ask("Спросить", Icons.Filled.Info),
    Browse("Файлы", Icons.Filled.List),
}

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            NotesTheme {
                val clients = remember { GrpcClients(applicationContext) }
                DisposableEffect(clients) {
                    onDispose { clients.shutdown() }
                }
                val userId = remember {
                    applicationContext.getString(R.string.user_id).toLongOrNull() ?: 0L
                }
                MainScreen(clients = clients, userId = userId)
            }
        }
    }
}

@Composable
fun MainScreen(
    clients: GrpcClients,
    userId: Long,
    modifier: Modifier = Modifier,
) {
    var selectedTab by rememberSaveable { mutableStateOf(Tab.Day) }
    // "" means "today"; Calendar taps set a concrete date to open in Day.
    var dayDate by rememberSaveable { mutableStateOf("") }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        bottomBar = {
            NavigationBar {
                Tab.entries.forEach { tab ->
                    NavigationBarItem(
                        selected = selectedTab == tab,
                        onClick = { selectedTab = tab },
                        icon = { Icon(tab.icon, contentDescription = tab.label) },
                        label = { Text(tab.label) },
                    )
                }
            }
        },
    ) { innerPadding ->
        Box(
            modifier = Modifier
                .padding(innerPadding)
                .fillMaxSize(),
        ) {
            when (selectedTab) {
                Tab.Day -> DayScreen(
                    core = clients.core,
                    date = dayDate,
                    onDateChange = { dayDate = it },
                )
                Tab.Calendar -> CalendarScreen(
                    core = clients.core,
                    onDayClick = { date ->
                        dayDate = date
                        selectedTab = Tab.Day
                    },
                )
                Tab.Reminders -> RemindersScreen(
                    notifications = clients.notifications,
                    llm = clients.llm,
                    userId = userId,
                )
                Tab.Search -> SearchScreen(
                    search = clients.search,
                    core = clients.core,
                )
                Tab.Ask -> AskScreen(search = clients.search)
                Tab.Browse -> BrowseScreen(core = clients.core)
            }
        }
    }
}