package app.folio.navigation

import kotlinx.serialization.Serializable

@Serializable
sealed interface Route {
    @Serializable data object Login : Route
    @Serializable data object Home : Route
    @Serializable data object AddAccount : Route
    @Serializable data class Ledger(val accountId: String) : Route
    @Serializable data class AddTransaction(val accountId: String) : Route
}
