package dev.uatu.sample

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

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
