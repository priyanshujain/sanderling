package app.folio.di

import app.folio.core.data.LedgerStore
import app.folio.core.data.Repository
import app.folio.core.data.SqlLedgerStore
import app.folio.db.LedgerDatabase
import app.folio.feature.account.AddAccountViewModel
import app.folio.feature.auth.LoginViewModel
import app.folio.feature.home.HomeViewModel
import app.folio.feature.ledger.AddTransactionViewModel
import app.folio.feature.ledger.LedgerViewModel
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import dev.zacsweers.metro.AppScope
import dev.zacsweers.metro.Binds
import dev.zacsweers.metro.DependencyGraph
import dev.zacsweers.metro.Provides
import dev.zacsweers.metro.SingleIn

@SingleIn(AppScope::class)
@DependencyGraph(AppScope::class)
interface AppGraph {
    val repository: Repository
    val navigator: Navigator

    val loginViewModel: LoginViewModel
    val homeViewModel: HomeViewModel
    val addAccountViewModel: AddAccountViewModel
    val ledgerViewModelFactory: LedgerViewModel.Factory
    val addTransactionViewModelFactory: AddTransactionViewModel.Factory

    @Binds val SqlLedgerStore.bindLedgerStore: LedgerStore

    @SingleIn(AppScope::class)
    @Provides
    fun provideNavigator(): Navigator = Navigator(initial = Route.Home)

    @DependencyGraph.Factory
    fun interface Factory {
        fun create(@Provides database: LedgerDatabase): AppGraph
    }
}
