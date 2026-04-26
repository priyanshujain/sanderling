@file:OptIn(ExperimentalComposeUiApi::class, kotlin.js.ExperimentalWasmJsInterop::class)

package app.folio

import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.window.ComposeViewport
import app.folio.core.data.DriverFactory
import app.folio.db.LedgerDatabase
import app.folio.di.AppGraph
import app.folio.ui.WebBackGesture
import dev.zacsweers.metro.createGraphFactory
import kotlinx.browser.document
import kotlinx.browser.window
import org.w3c.dom.events.Event

fun main() {
    val driverFactory = DriverFactory()
    val graphFactory = createGraphFactory<AppGraph.Factory>()

    window.history.pushState(null, "", window.location.href)
    window.addEventListener("popstate", { _: Event ->
        if (WebBackGesture.dispatch()) {
            window.history.pushState(null, "", window.location.href)
        }
    })

    val target = document.getElementById("app") ?: document.body!!
    ComposeViewport(target) {
        App(graphBuilder = {
            graphFactory.create(LedgerDatabase(driverFactory.create()))
        })
    }
}
