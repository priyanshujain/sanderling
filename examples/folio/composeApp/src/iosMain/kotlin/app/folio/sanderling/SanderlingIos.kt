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
        val port = resolvePort() ?: return
        IosAgent.start("127.0.0.1", port)
    }

    private fun resolvePort(): Int? {
        // Env var set via SIMCTL_CHILD_SANDERLING_PORT (simctl direct launch).
        (NSProcessInfo.processInfo.environment["SANDERLING_PORT"] as? String)
            ?.toIntOrNull()?.let { return it }
        // Launch argument -SANDERLING_PORT <value> (Maestro simctl launch).
        @Suppress("UNCHECKED_CAST")
        val args = NSProcessInfo.processInfo.arguments as? List<String> ?: return null
        val idx = args.indexOfFirst { it == "-SANDERLING_PORT" }
        if (idx >= 0 && idx + 1 < args.size) {
            return args[idx + 1].toIntOrNull()
        }
        return null
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
