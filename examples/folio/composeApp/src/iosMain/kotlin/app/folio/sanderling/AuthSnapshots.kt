package app.folio.sanderling

import app.folio.data.Repository
import app.folio.feature.auth.LoginUiState

object AuthSnapshots {
    val loggedIn by SanderlingIos.snapshot { Repository.session.value != null }
    val authStatus by SanderlingIos.snapshot { if (Repository.session.value != null) "logged-in" else "logged-out" }
    val loginError by SanderlingIos.snapshot { LoginUiState.loginError.value }
}
