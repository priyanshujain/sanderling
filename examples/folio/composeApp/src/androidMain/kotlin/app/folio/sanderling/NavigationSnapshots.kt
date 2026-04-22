package app.folio.sanderling

import app.folio.navigation.Navigator
import app.folio.navigation.Route
import dev.sanderling.sdk.Sanderling

object NavigationSnapshots {
    val screen by Sanderling.snapshot {
        when (Navigator.current.value) {
            Route.Login -> "login"
            Route.Home -> "home"
            Route.AddAccount -> "add-account"
            is Route.Ledger -> "ledger"
            is Route.AddTransaction -> "add-transaction"
        }
    }
}
