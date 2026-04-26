package app.folio.di

import androidx.compose.runtime.staticCompositionLocalOf

val LocalAppGraph = staticCompositionLocalOf<AppGraph> {
    error("AppGraph not provided. Wrap content in CompositionLocalProvider(LocalAppGraph provides ...).")
}
