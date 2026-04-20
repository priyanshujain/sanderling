package app.folio

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

// Assumes single-focus: only one input can be focused at a time, so
// enter() overwriting is safe. leave() is id-gated so a stale dispose
// from a previously-focused field cannot clobber the active focus.
object FocusTracker {
    private val _current = MutableStateFlow<String?>(null)
    val current: StateFlow<String?> = _current.asStateFlow()

    fun enter(id: String) {
        _current.value = id
    }

    fun leave(id: String) {
        if (_current.value == id) _current.value = null
    }
}
