package dev.uatu.sample

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import dev.uatu.sample.ui.AddAccountPage
import dev.uatu.sample.ui.AddTransactionPage
import dev.uatu.sample.ui.HomePage
import dev.uatu.sample.ui.LedgerPage
import dev.uatu.sample.ui.LedgerTheme
import dev.uatu.sample.ui.LocalTokens
import dev.uatu.sample.ui.LoginPage
import dev.uatu.sample.ui.Tokens

@Composable
fun App() {
    LaunchedEffect(Unit) { Repository.load() }
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
        androidx.compose.runtime.CompositionLocalProvider(LocalTokens provides t) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(t.bg)
                    .windowInsetsPadding(WindowInsets.safeDrawing),
            ) {
                Column(Modifier.fillMaxSize()) {
                    when (val r = route) {
                        Route.Login -> LoginPage(onLoggedIn = { Navigator.replace(Route.Home) })
                        Route.Home -> HomePage(
                            user = session?.user ?: "",
                            onLogout = {
                                Repository.clearSession()
                                Navigator.replace(Route.Login)
                            },
                        )
                        Route.AddAccount -> AddAccountPage()
                        is Route.Ledger -> LedgerPage(accountId = r.accountId)
                        is Route.AddTransaction -> AddTransactionPage(accountId = r.accountId)
                    }
                }
            }
        }
    }
}
