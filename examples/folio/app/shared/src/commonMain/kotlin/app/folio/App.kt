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
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import app.folio.core.data.DriverFactory
import app.folio.core.data.Repository
import app.folio.core.data.SqlLedgerStore
import app.folio.db.LedgerDatabase
import app.folio.feature.account.AddAccountRoute
import app.folio.feature.auth.LoginRoute
import app.folio.feature.home.HomeRoute
import app.folio.feature.ledger.AddTransactionRoute
import app.folio.feature.ledger.LedgerRoute
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.ui.theme.LedgerTheme
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Tokens

@OptIn(ExperimentalComposeUiApi::class)
@Composable
fun App(driverFactory: DriverFactory) {
    var component by remember { mutableStateOf<AppComponent?>(null) }

    LaunchedEffect(Unit) {
        val driver = driverFactory.create()
        val db = LedgerDatabase(driver)
        val store = SqlLedgerStore(db)
        component = AppComponent(repository = Repository(store), navigator = Navigator(initial = Route.Home))
    }

    LedgerTheme {
        val t = Tokens()
        CompositionLocalProvider(LocalTokens provides t) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(t.bg)
                    .windowInsetsPadding(WindowInsets.safeDrawing)
                    .semantics { testTagsAsResourceId = true },
                contentAlignment = Alignment.Center,
            ) {
                val c = component
                if (c == null) {
                    // Loading: keep blank to avoid surprising the spec runner.
                } else {
                    CompositionLocalProvider(LocalAppComponent provides c) {
                        AppContent()
                    }
                }
            }
        }
    }
}

@Composable
private fun AppContent() {
    val component = LocalAppComponent.current
    val session by component.repository.session.collectAsState()
    val route by component.navigator.current.collectAsState()

    LaunchedEffect(session, route) {
        if (session == null && route !is Route.Login) {
            component.navigator.replace(Route.Login)
        } else if (session != null && route is Route.Login) {
            component.navigator.replace(Route.Home)
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
