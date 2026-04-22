package app.folio.sanderling

import app.folio.data.Repository
import app.folio.feature.auth.LoginUiState
import dev.sanderling.sdk.Sanderling

object AuthSnapshots {
    val loggedIn by Sanderling.snapshot { Repository.session.value != null }
    val authStatus by Sanderling.snapshot { if (Repository.session.value != null) "logged-in" else "logged-out" }
    val loginError by Sanderling.snapshot { LoginUiState.loginError.value }
}
