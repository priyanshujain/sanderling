package app.folio.sanderling

import kotlin.properties.ReadOnlyProperty
import kotlin.reflect.KProperty
import platform.Foundation.NSProcessInfo

private fun String.camelToSnakeCase(): String = buildString {
    for ((i, c) in this@camelToSnakeCase.withIndex()) {
        if (c.isUpperCase() && i > 0) append('_')
        append(c.lowercaseChar())
    }
}

object SanderlingIos {
    internal val extractors = mutableMapOf<String, () -> Any?>()

    fun start() {
        val env = NSProcessInfo.processInfo.environment
        val portStr = env["SANDERLING_PORT"] as? String ?: return
        val port = portStr.toIntOrNull() ?: return
        IosAgent.start("127.0.0.1", port)
    }

    fun extract(name: String, block: () -> Any?) {
        extractors[name] = block
    }

    fun <T> snapshot(block: () -> T): SnapshotDelegate<T> = SnapshotDelegate(block)
}

class SnapshotDelegate<T>(private val block: () -> T) {
    operator fun provideDelegate(thisRef: Any?, prop: KProperty<*>): ReadOnlyProperty<Any?, T> {
        SanderlingIos.extract(prop.name.camelToSnakeCase(), block as () -> Any?)
        return ReadOnlyProperty { _, _ -> block() }
    }
}
