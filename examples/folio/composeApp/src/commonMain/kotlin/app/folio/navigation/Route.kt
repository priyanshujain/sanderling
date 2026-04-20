package app.folio.navigation

sealed interface Route {
    data object Login : Route
    data object Home : Route
    data object AddAccount : Route
    data class Ledger(val accountId: String) : Route
    data class AddTransaction(val accountId: String) : Route
}
