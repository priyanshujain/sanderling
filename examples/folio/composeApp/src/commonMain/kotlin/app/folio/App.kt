package app.folio

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import app.folio.data.Repository
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.ui.AddAccountScreen
import app.folio.ui.AddTransactionScreen
import app.folio.ui.HomeScreen
import app.folio.ui.LedgerScreen
import app.folio.ui.LedgerTheme
import app.folio.ui.LocalTokens
import app.folio.ui.LoginScreen
import app.folio.ui.Tokens

@Composable
fun App() {
    val session by Repository.session.collectAsState()
    val route by Navigator.current.collectAsState()

    LaunchedEffect(session, route) {
        if (session == null && route !is Route.Login) {
            Navigator.replace(Route.Login)
        } else if (session != null && route is Route.Login) {
            Navigator.replace(Route.Home)
        }
    }

    LedgerTheme {
        val t = Tokens()
        CompositionLocalProvider(LocalTokens provides t) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(t.bg)
                    .windowInsetsPadding(WindowInsets.safeDrawing),
            ) {
                Column(Modifier.fillMaxSize()) {
                    when (val r = route) {
                        Route.Login -> LoginScreen(onLoggedIn = { Navigator.replace(Route.Home) })
                        Route.Home -> HomeScreen(
                            user = session?.user ?: "",
                            onLogout = {
                                Repository.clearSession()
                                Navigator.replace(Route.Login)
                            },
                        )
                        Route.AddAccount -> AddAccountScreen()
                        is Route.Ledger -> LedgerScreen(accountId = r.accountId)
                        is Route.AddTransaction -> AddTransactionScreen(accountId = r.accountId)
                    }
                }
            }
        }
    }
}
