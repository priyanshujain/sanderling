package dev.uatu.sample

import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.window.ComposeViewport
import dev.uatu.sample.ui.WebBackGesture
import kotlinx.browser.document
import kotlinx.browser.window
import org.w3c.dom.events.Event

@OptIn(ExperimentalComposeUiApi::class)
fun main() {
    Repository.init()
    window.history.pushState(null, "", window.location.href)
    window.addEventListener("popstate", { _: Event ->
        if (WebBackGesture.dispatch()) {
            window.history.pushState(null, "", window.location.href)
        }
    })
    val target = document.getElementById("app") ?: document.body!!
    ComposeViewport(target) { App() }
}
