package dev.uatu.sidecar

interface DriverBackend {
    fun launch(bundleId: String, clearState: Boolean)
    fun terminate(bundleId: String)
    fun tap(x: Int, y: Int)
    fun tapSelector(selector: String)
    fun inputText(text: String)
    fun screenshot(): Triple<ByteArray, Int, Int>
    fun hierarchy(): String
    fun waitForIdle(durationMillis: Long)
    fun healthy(): Boolean
}

/**
 * StubDriverBackend records calls but takes no real device action. Real
 * Maestro integration arrives in a follow-up; v0.1 wires the gRPC plumbing
 * end-to-end so the Go side can be exercised against a running sidecar
 * even before Maestro is plugged in.
 */
class StubDriverBackend(private val platform: String) : DriverBackend {
    @Volatile var launchCount: Int = 0
        private set
    @Volatile var lastBundleId: String? = null
        private set
    @Volatile var lastTap: Pair<Int, Int>? = null
        private set
    @Volatile var lastTapSelector: String? = null
        private set
    @Volatile var lastInputText: String? = null
        private set

    override fun launch(bundleId: String, clearState: Boolean) {
        launchCount++
        lastBundleId = bundleId
    }

    override fun terminate(bundleId: String) {
        lastBundleId = null
    }

    override fun tap(x: Int, y: Int) {
        lastTap = x to y
    }

    override fun tapSelector(selector: String) {
        lastTapSelector = selector
    }

    override fun inputText(text: String) {
        lastInputText = text
    }

    override fun screenshot(): Triple<ByteArray, Int, Int> = Triple(ByteArray(0), 0, 0)

    override fun hierarchy(): String = "{\"children\":[],\"platform\":\"$platform\"}"

    override fun waitForIdle(durationMillis: Long) {
        if (durationMillis > 0) Thread.sleep(durationMillis)
    }

    override fun healthy(): Boolean = true
}
