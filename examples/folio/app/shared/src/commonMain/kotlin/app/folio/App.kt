package app.folio

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
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
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
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
    val navController = rememberNavController()

    LaunchedEffect(navController) { graph.navigator.attach(navController) }

    val session by graph.repository.session.collectAsState()
    val currentEntry by navController.currentBackStackEntryAsState()
    val onLogin = currentEntry?.destination?.hasRoute(Route.Login::class) == true

    LaunchedEffect(session, onLogin) {
        if (session == null && !onLogin) {
            graph.navigator.replace(Route.Login)
        } else if (session != null && onLogin) {
            graph.navigator.replace(Route.Home)
        }
    }

    NavHost(
        navController = navController,
        startDestination = Route.Home,
        modifier = Modifier.fillMaxSize(),
    ) {
        composable<Route.Login> { LoginRoute() }
        composable<Route.Home> { HomeRoute() }
        composable<Route.AddAccount> { AddAccountRoute() }
        composable<Route.Ledger> { entry ->
            LedgerRoute(accountId = entry.toRoute<Route.Ledger>().accountId)
        }
        composable<Route.AddTransaction> { entry ->
            AddTransactionRoute(accountId = entry.toRoute<Route.AddTransaction>().accountId)
        }
    }
}
