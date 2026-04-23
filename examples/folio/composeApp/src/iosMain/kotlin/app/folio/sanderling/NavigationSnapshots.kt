package app.folio.sanderling

import app.folio.navigation.Navigator
import app.folio.navigation.Route

object NavigationSnapshots {
    val screen by SanderlingIos.snapshot {
        when (Navigator.current.value) {
            Route.Login -> "login"
            Route.Home -> "home"
            Route.AddAccount -> "add-account"
            is Route.Ledger -> "ledger"
            is Route.AddTransaction -> "add-transaction"
        }
    }
}
