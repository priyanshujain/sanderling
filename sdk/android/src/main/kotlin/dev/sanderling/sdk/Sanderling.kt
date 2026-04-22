package dev.sanderling.sdk

import android.app.Application
import android.util.Log
import kotlin.properties.ReadOnlyProperty
import kotlin.reflect.KProperty

internal fun String.camelToSnakeCase(): String = buildString {
    for ((i, c) in this@camelToSnakeCase.withIndex()) {
        if (c.isUpperCase() && i > 0) append('_')
        append(c.lowercaseChar())
    }
}

data class Configuration(
    val socketName: String = "sanderling-agent",
    val pauseTimeoutMillis: Long = 5_000L,
)

object Sanderling {
    const val VERSION: String = "0.0.1"
    private const val LOG_TAG = "Sanderling"

    @Volatile private var runtime: SanderlingRuntime? = null

    @JvmOverloads
    @Synchronized
    fun start(application: Application, configuration: Configuration = Configuration()) {
        if (runtime != null) return
        val newRuntime = SanderlingRuntime(
            transport = LocalAbstractTransport(configuration.socketName),
            pauser = Pauser(ChoreographerPoster(), configuration.pauseTimeoutMillis),
            version = VERSION,
            platform = "android",
            appPackage = application.packageName,
        )
        newRuntime.start()
        runtime = newRuntime
        Log.i(LOG_TAG, "SDK started (package=${application.packageName} socket=${configuration.socketName})")
    }

    fun extract(name: String, function: () -> Any?) {
        val activeRuntime = runtime
            ?: throw IllegalStateException("Sanderling.start must be called before registering extractors")
        activeRuntime.register(name, function)
    }

    fun <T> snapshot(function: () -> T): SnapshotDelegate<T> = SnapshotDelegate(function)

    /**
     * Records a caught [Throwable] so it surfaces in the next STATE message's
     * exceptions field. Useful for coroutine CoroutineExceptionHandler,
     * OkHttp interceptors, or anywhere else the host app catches errors it
     * still wants verified against properties like noUncaughtExceptions.
     */
    fun reportError(throwable: Throwable) {
        runtime?.reportError(throwable)
    }

    @Synchronized
    internal fun stopForTest() {
        runtime?.stop()
        runtime = null
    }
}

class SnapshotDelegate<T>(private val function: () -> T) {
    operator fun provideDelegate(thisRef: Any?, prop: KProperty<*>): ReadOnlyProperty<Any?, T> {
        Sanderling.extract(prop.name.camelToSnakeCase(), function as () -> Any?)
        return ReadOnlyProperty { _, _ -> function() }
    }
}
