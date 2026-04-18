package dev.uatu.sample

import android.app.Application
import dev.uatu.sdk.Uatu

class SampleApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        Uatu.start(this)
        Uatu.extract("app_state") { "running" }
        Uatu.extract("click_count") { MainActivity.clickCount }
        Uatu.extract("username") { MainActivity.username }
        Uatu.extract("uptime_millis") { System.currentTimeMillis() - startedAt }
    }

    private val startedAt: Long = System.currentTimeMillis()
}
