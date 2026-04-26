package app.folio

import androidx.compose.runtime.staticCompositionLocalOf
import app.folio.core.data.Repository
import app.folio.navigation.Navigator

class AppComponent(
    val repository: Repository,
    val navigator: Navigator,
)

val LocalAppComponent = staticCompositionLocalOf<AppComponent> {
    error("AppComponent not provided. Wrap content in CompositionLocalProvider(LocalAppComponent provides ...).")
}
