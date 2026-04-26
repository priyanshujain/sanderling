package app.folio.navigation

import androidx.navigation.NavHostController

class Navigator {
    private var controller: NavHostController? = null

    fun attach(controller: NavHostController) {
        this.controller = controller
    }

    fun push(route: Route) {
        controller?.navigate(route) {
            launchSingleTop = true
        }
    }

    fun replace(route: Route) {
        val nav = controller ?: return
        nav.navigate(route) {
            popUpTo(nav.graph.id) { inclusive = true }
            launchSingleTop = true
        }
    }

    fun back(fallback: Route) {
        val nav = controller ?: return
        if (!nav.popBackStack()) {
            replace(fallback)
        }
    }
}
