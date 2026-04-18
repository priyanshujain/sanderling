package dev.uatu.sdk

import android.app.Application
import android.util.Log

data class Configuration(
    val socketName: String = "uatu-agent",
    val pauseTimeoutMillis: Long = 5_000L,
)

object Uatu {
    const val VERSION: String = "0.0.1"
    private const val LOG_TAG = "Uatu"

    @Volatile private var runtime: UatuRuntime? = null

    @JvmOverloads
    @Synchronized
    fun start(application: Application, configuration: Configuration = Configuration()) {
        if (runtime != null) return
        val newRuntime = UatuRuntime(
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
            ?: throw IllegalStateException("Uatu.start must be called before registering extractors")
        activeRuntime.register(name, function)
    }

    @Synchronized
    internal fun stopForTest() {
        runtime?.stop()
        runtime = null
    }
}
