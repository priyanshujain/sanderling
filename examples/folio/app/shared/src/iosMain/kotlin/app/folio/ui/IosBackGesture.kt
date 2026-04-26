package app.folio.ui

object IosBackGesture {
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
