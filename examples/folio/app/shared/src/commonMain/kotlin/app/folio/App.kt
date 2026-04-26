package app.folio

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import app.folio.di.AppGraph
import app.folio.di.LocalAppGraph
import app.folio.feature.account.AddAccountRoute
import app.folio.feature.auth.LoginRoute
import app.folio.feature.home.HomeRoute
import app.folio.feature.ledger.AddTransactionRoute
import app.folio.feature.ledger.LedgerRoute
import app.folio.navigation.Route
import app.folio.ui.testTagsAsResourceId
import app.folio.ui.theme.LedgerTheme
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Tokens

@Composable
fun App(graphBuilder: suspend () -> AppGraph) {
    var graph by remember { mutableStateOf<AppGraph?>(null) }

    LaunchedEffect(Unit) { graph = graphBuilder() }

    LedgerTheme {
        val t = Tokens()
        CompositionLocalProvider(LocalTokens provides t) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(t.bg)
                    .windowInsetsPadding(WindowInsets.safeDrawing)
                    .testTagsAsResourceId(),
                contentAlignment = Alignment.Center,
            ) {
                val g = graph
                if (g != null) {
                    CompositionLocalProvider(LocalAppGraph provides g) {
                        AppContent()
                    }
                }
            }
        }
    }
}

@Composable
private fun AppContent() {
    val graph = LocalAppGraph.current
    val session by graph.repository.session.collectAsState()
    val route by graph.navigator.current.collectAsState()

    LaunchedEffect(session, route) {
        if (session == null && route !is Route.Login) {
            graph.navigator.replace(Route.Login)
        } else if (session != null && route is Route.Login) {
            graph.navigator.replace(Route.Home)
        }
    }

    Column(Modifier.fillMaxSize()) {
        when (val r = route) {
            Route.Login -> LoginRoute()
            Route.Home -> HomeRoute()
            Route.AddAccount -> AddAccountRoute()
            is Route.Ledger -> LedgerRoute(accountId = r.accountId)
            is Route.AddTransaction -> AddTransactionRoute(accountId = r.accountId)
        }
    }
}
