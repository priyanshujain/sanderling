package app.folio.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState

@Composable
actual fun BackHandler(onBack: () -> Unit) {
    val current by rememberUpdatedState(onBack)
    DisposableEffect(Unit) {
        val id = WebBackGesture.register { current() }
        onDispose { WebBackGesture.unregister(id) }
    }
}

object WebBackGesture {
    private var nextId = 0
    private val callbacks = mutableListOf<Pair<Int, () -> Unit>>()

    fun register(onBack: () -> Unit): Int {
        val id = nextId++
        callbacks.add(id to onBack)
        return id
    }

    fun unregister(id: Int) {
        callbacks.removeAll { it.first == id }
    }

    fun dispatch(): Boolean {
        val top = callbacks.lastOrNull() ?: return false
        top.second()
        return true
    }
}
