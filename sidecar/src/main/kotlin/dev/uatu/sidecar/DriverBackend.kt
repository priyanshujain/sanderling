package dev.uatu.sidecar

interface DriverBackend {
    fun launch(bundleId: String, launcherActivity: String, clearState: Boolean)
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

    override fun launch(bundleId: String, launcherActivity: String, clearState: Boolean) {
        launchCount++
        lastBundleId = bundleId
        if (clearState) {
            runAdb(listOf("shell", "pm", "clear", bundleId))
        }
        if (launcherActivity.isNotEmpty()) {
            val component = if (launcherActivity.contains('/')) launcherActivity else "$bundleId/$launcherActivity"
            runAdb(listOf("shell", "am", "start", "-n", component))
        } else {
            // `monkey` uses PackageManager.getLaunchIntentForPackage, which
            // picks the canonical default launcher.
            runAdb(listOf("shell", "monkey", "-p", bundleId, "-c", "android.intent.category.LAUNCHER", "1"))
        }
    }

    override fun terminate(bundleId: String) {
        runAdb(listOf("shell", "am", "force-stop", bundleId))
        lastBundleId = null
    }

    override fun tap(x: Int, y: Int) {
        lastTap = x to y
        runAdb(listOf("shell", "input", "tap", x.toString(), y.toString()))
    }

    override fun tapSelector(selector: String) {
        lastTapSelector = selector
        // v0.1: selector resolution lives in Maestro proper; the stub
        // records the selector so logs/traces show what was requested.
    }

    override fun inputText(text: String) {
        lastInputText = text
        runAdb(listOf("shell", "input", "text", text.replace(" ", "%s")))
    }

    private fun runAdb(arguments: List<String>) {
        try {
            val command = ProcessBuilder(listOf("adb") + arguments).redirectErrorStream(true).start()
            // Drain output before waiting so a large write doesn't block the child.
            command.inputStream.bufferedReader().readText()
            command.waitFor()
        } catch (cause: Exception) {
            println("adb ${arguments.joinToString(" ")} failed: $cause")
        }
    }

    override fun screenshot(): Triple<ByteArray, Int, Int> = Triple(ByteArray(0), 0, 0)

    override fun hierarchy(): String {
        return try {
            val process = ProcessBuilder(
                listOf(
                    "adb", "shell",
                    "uiautomator dump /sdcard/window_dump.xml >/dev/null 2>&1 && cat /sdcard/window_dump.xml",
                ),
            ).redirectErrorStream(false).start()
            val output = process.inputStream.bufferedReader().readText()
            process.waitFor()
            if (output.isBlank()) "<hierarchy/>" else output
        } catch (cause: Exception) {
            println("adb uiautomator dump failed: $cause")
            "<hierarchy/>"
        }
    }

    override fun waitForIdle(durationMillis: Long) {
        if (durationMillis > 0) Thread.sleep(durationMillis)
    }

    override fun healthy(): Boolean = true
}
