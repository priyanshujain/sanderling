package app.folio.navigation

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

object Navigator {
    private val stack = ArrayDeque<Route>().apply { addLast(Route.Home) }
    private val _current = MutableStateFlow<Route>(Route.Home)
    val current: StateFlow<Route> = _current.asStateFlow()

    fun push(route: Route) {
        stack.addLast(route)
        _current.value = route
    }

    fun replace(route: Route) {
        stack.clear()
        stack.addLast(route)
        _current.value = route
    }

    fun back(fallback: Route) {
        if (stack.size > 1) {
            stack.removeLast()
            _current.value = stack.last()
        } else {
            replace(fallback)
        }
    }
}
