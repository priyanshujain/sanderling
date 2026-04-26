package app.folio.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState

@Composable
actual fun BackHandler(onBack: () -> Unit) {
    val current by rememberUpdatedState(onBack)
    DisposableEffect(Unit) {
        val id = IosBackGesture.register { current() }
        onDispose { IosBackGesture.unregister(id) }
    }
}
