package dev.uatu.sdk

import android.app.Application

object Uatu {
    fun start(application: Application, configuration: Configuration = Configuration()) {
        // stub — real implementation in tasks #6–8
    }

    fun extract(name: String, function: () -> Any?) {
        // stub — registry lands in task #8
    }
}

data class Configuration(
    val socketName: String = "uatu-agent",
    val pauseTimeoutMillis: Long = 5_000L,
)
