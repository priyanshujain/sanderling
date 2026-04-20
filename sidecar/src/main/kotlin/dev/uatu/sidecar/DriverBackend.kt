package dev.uatu.sidecar

interface DriverBackend {
    fun launch(bundleId: String, launcherActivity: String, clearState: Boolean)
    fun terminate(bundleId: String)
    fun tap(x: Int, y: Int)
    fun tapSelector(selector: String)
    fun inputText(text: String)
    fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long)
    fun pressKey(key: String)
    fun screenshot(): Triple<ByteArray, Int, Int>
    fun hierarchy(): String
    fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine>
    fun waitForIdle(durationMillis: Long)
    fun healthy(): Boolean
    fun metrics(bundleId: String): MetricsSample
}

data class MetricsSample(
    val cpuPercent: Double,
    val heapBytes: Long,
    val totalMemoryBytes: Long,
)

data class LogLine(
    val unixMillis: Long,
    val level: String,
    val tag: String,
    val message: String,
)

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
        val component = when {
            launcherActivity.isEmpty() -> "$bundleId/${resolveLauncherActivity(bundleId)}"
            launcherActivity.contains('/') -> launcherActivity
            else -> "$bundleId/$launcherActivity"
        }
        runAdb(listOf("shell", "am", "start", "-W", "-n", component))
    }

    private fun resolveLauncherActivity(bundleId: String): String {
        val output = captureAdb(
            listOf(
                "shell", "cmd", "package", "resolve-activity", "--brief",
                "-a", "android.intent.action.MAIN",
                "-c", "android.intent.category.LAUNCHER",
                bundleId,
            ),
        )
        return parseResolvedActivity(bundleId, output)
            ?: throw IllegalStateException("could not resolve launcher activity for $bundleId: $output")
    }

    private fun captureAdb(arguments: List<String>): String {
        val process = ProcessBuilder(listOf("adb") + arguments).redirectErrorStream(true).start()
        val output = process.inputStream.bufferedReader().readText()
        process.waitFor()
        return output
    }

    companion object {
        // parseResolvedActivity extracts the activity name from the output of
        // `cmd package resolve-activity --brief`. The brief output is two
        // lines: metadata, then `<pkg>/<activity>`.
        internal fun parseResolvedActivity(bundleId: String, output: String): String? {
            val prefix = "$bundleId/"
            for (line in output.lines()) {
                val trimmed = line.trim()
                if (trimmed.startsWith(prefix)) {
                    return trimmed.removePrefix(prefix)
                }
            }
            return null
        }

        // Hard cap on KEYCODE_DEL events per clear. Guards against a pathological
        // hierarchy that reports an enormous text length for the focused field.
        internal const val MAX_CLEAR_DELETES: Int = 1024

        internal fun buildClearKeyevents(textLength: Int): List<String> {
            if (textLength <= 0) return emptyList()
            val deletes = minOf(textLength, MAX_CLEAR_DELETES)
            val args = mutableListOf("shell", "input", "keyevent", "KEYCODE_MOVE_END")
            repeat(deletes) { args.add("KEYCODE_DEL") }
            return args
        }

        // `adb shell input text` runs through a remote sh, so shell metacharacters
        // in the payload would be interpreted by the device shell. Substitute
        // spaces with %s (input's escape) and backslash-escape characters sh
        // would otherwise expand. Keep this list conservative; anything not
        // listed passes through literally.
        internal fun escapeForAdbInputText(text: String): String {
            val sb = StringBuilder(text.length)
            for (ch in text) {
                when (ch) {
                    ' ' -> sb.append("%s")
                    '\\', '"', '\'', '&', '|', ';', '<', '>', '(', ')', '*', '?',
                    '$', '`', '[', ']', '{', '}', '~', '#', -> sb.append('\\').append(ch)
                    else -> sb.append(ch)
                }
            }
            return sb.toString()
        }

        // Matches a uiautomator-dump <node ...> tag where `focused="true"` is
        // present. Captures only the tag's attribute string so we can pull
        // `text="..."` out of it without building a full XML tree.
        private val FOCUSED_NODE = Regex(
            "<node\\b([^>]*\\bfocused=\"true\"[^>]*)/?>",
        )
        private val TEXT_ATTRIBUTE = Regex("\\btext=\"([^\"]*)\"")

        internal fun parseFocusedText(xml: String): String? {
            val node = FOCUSED_NODE.find(xml) ?: return null
            val match = TEXT_ATTRIBUTE.find(node.groupValues[1]) ?: return ""
            return decodeXmlAttribute(match.groupValues[1])
        }

        private fun decodeXmlAttribute(value: String): String = value
            .replace("&amp;", "&")
            .replace("&lt;", "<")
            .replace("&gt;", ">")
            .replace("&quot;", "\"")
            .replace("&apos;", "'")

        internal val KEY_MAP: Map<String, String> = mapOf(
            "back" to "KEYCODE_BACK",
            "home" to "KEYCODE_HOME",
            "enter" to "KEYCODE_ENTER",
            "tab" to "KEYCODE_TAB",
            "up" to "KEYCODE_DPAD_UP",
            "down" to "KEYCODE_DPAD_DOWN",
            "left" to "KEYCODE_DPAD_LEFT",
            "right" to "KEYCODE_DPAD_RIGHT",
        )

        internal fun formatAdbLogcatTimestamp(unixMillis: Long): String {
            val seconds = unixMillis / 1000
            val millis = unixMillis % 1000
            return "$seconds.${millis.toString().padStart(3, '0')}"
        }

        // Logcat default threadtime format:
        //   MM-dd HH:mm:ss.SSS  PID  TID L TAG: message
        // The leading date is the local year-inferred date; we convert to a
        // unix-millis best-effort using the current year.
        private val LOGCAT_LINE = Regex(
            "^(\\d{2})-(\\d{2}) (\\d{2}):(\\d{2}):(\\d{2})\\.(\\d{3})" +
                "\\s+\\d+\\s+\\d+\\s+([VDIWEFS])\\s+([^:]+?):\\s?(.*)$",
        )

        internal fun parseLogcatOutput(output: String): List<LogLine> {
            if (output.isBlank()) return emptyList()
            val calendar = java.util.Calendar.getInstance()
            val year = calendar.get(java.util.Calendar.YEAR)
            val result = mutableListOf<LogLine>()
            for (line in output.lines()) {
                val match = LOGCAT_LINE.matchEntire(line) ?: continue
                val month = match.groupValues[1].toInt() - 1
                val day = match.groupValues[2].toInt()
                val hour = match.groupValues[3].toInt()
                val minute = match.groupValues[4].toInt()
                val second = match.groupValues[5].toInt()
                val millis = match.groupValues[6].toInt()
                val level = match.groupValues[7]
                val tag = match.groupValues[8].trim()
                val message = match.groupValues[9]
                calendar.clear()
                calendar.set(year, month, day, hour, minute, second)
                calendar.set(java.util.Calendar.MILLISECOND, millis)
                result.add(LogLine(calendar.timeInMillis, level, tag, message))
            }
            return result
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
        // `adb shell input text` types keystrokes at the caret, so repeated
        // calls append. Clear the focused field first so the caller sees a
        // pure replace: read the current value's length from the hierarchy,
        // then move-end + N backspaces before typing.
        clearFocusedField()
        runAdb(listOf("shell", "input", "text", escapeForAdbInputText(text)))
    }

    private fun clearFocusedField() {
        val current = focusedFieldText() ?: return
        if (current.isEmpty()) return
        runAdb(buildClearKeyevents(current.length))
    }

    private fun focusedFieldText(): String? {
        val xml = try {
            hierarchy()
        } catch (cause: Exception) {
            println("inputText: hierarchy dump failed: $cause")
            return null
        }
        if (xml.isBlank() || xml == "<hierarchy/>") return null
        return parseFocusedText(xml)
    }

    @Volatile var lastSwipe: SwipeRecord? = null
        private set
    @Volatile var lastKey: String? = null
        private set

    override fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long) {
        lastSwipe = SwipeRecord(fromX, fromY, toX, toY, durationMillis)
        val effectiveDuration = if (durationMillis > 0) durationMillis else 250L
        runAdb(
            listOf(
                "shell", "input", "swipe",
                fromX.toString(), fromY.toString(),
                toX.toString(), toY.toString(),
                effectiveDuration.toString(),
            ),
        )
    }

    override fun pressKey(key: String) {
        lastKey = key
        val keyCode = KEY_MAP[key.lowercase()]
            ?: throw IllegalArgumentException("unsupported pressKey value: $key")
        runAdb(listOf("shell", "input", "keyevent", keyCode))
    }

    override fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine> {
        val level = if (minLevel.isEmpty()) "E" else minLevel
        val since = if (sinceUnixMillis > 0) formatAdbLogcatTimestamp(sinceUnixMillis) else null
        val arguments = mutableListOf("logcat", "-d", "*:$level")
        if (since != null) {
            arguments.add("-T")
            arguments.add(since)
        }
        return try {
            val process = ProcessBuilder(listOf("adb") + arguments).redirectErrorStream(false).start()
            val output = process.inputStream.bufferedReader().readText()
            process.waitFor()
            parseLogcatOutput(output)
        } catch (cause: Exception) {
            println("adb logcat failed: $cause")
            emptyList()
        }
    }

    data class SwipeRecord(val fromX: Int, val fromY: Int, val toX: Int, val toY: Int, val durationMillis: Long)

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

    override fun screenshot(): Triple<ByteArray, Int, Int> {
        return try {
            val process = ProcessBuilder(listOf("adb", "exec-out", "screencap", "-p"))
                .redirectErrorStream(false)
                .start()
            val png = process.inputStream.readAllBytes()
            process.waitFor()
            if (png.isEmpty()) Triple(ByteArray(0), 0, 0) else Triple(png, 0, 0)
        } catch (cause: Exception) {
            println("adb screencap failed: $cause")
            Triple(ByteArray(0), 0, 0)
        }
    }

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

    override fun metrics(bundleId: String): MetricsSample {
        if (bundleId.isEmpty()) return MetricsSample(0.0, 0L, 0L)
        return try {
            val pid = runAdbOutput(listOf("shell", "pidof", bundleId)).trim().split(Regex("\\s+")).firstOrNull()?.toIntOrNull()
                ?: return MetricsSample(0.0, 0L, 0L)
            val cpu = sampleCpuPercent(pid)
            val (rssBytes, vmSizeBytes) = sampleProcessMemory(pid)
            MetricsSample(cpu, rssBytes, vmSizeBytes)
        } catch (cause: Exception) {
            println("metrics capture failed: $cause")
            MetricsSample(0.0, 0L, 0L)
        }
    }

    private fun sampleCpuPercent(pid: Int): Double {
        // Use `top -n 1 -p <pid>` which samples in a short window. Output format:
        //   "PID USER PR NI VIRT RES SHR S %CPU %MEM TIME+ COMMAND"
        val output = runAdbOutput(listOf("shell", "top", "-b", "-n", "1", "-p", pid.toString()))
        val line = output.lineSequence()
            .map(String::trim)
            .firstOrNull { it.startsWith(pid.toString()) }
            ?: return 0.0
        val columns = line.split(Regex("\\s+"))
        // [0] PID [1] USER [2] PR [3] NI [4] VIRT [5] RES [6] SHR [7] S [8] %CPU
        if (columns.size < 9) return 0.0
        return columns[8].toDoubleOrNull() ?: 0.0
    }

    private fun sampleProcessMemory(pid: Int): Pair<Long, Long> {
        val status = runAdbOutput(listOf("shell", "cat", "/proc/$pid/status"))
        var rssKb = 0L
        var vmSizeKb = 0L
        for (raw in status.lineSequence()) {
            val line = raw.trim()
            when {
                line.startsWith("VmRSS:") -> rssKb = parseKb(line) ?: rssKb
                line.startsWith("VmSize:") -> vmSizeKb = parseKb(line) ?: vmSizeKb
            }
        }
        return Pair(rssKb * 1024L, vmSizeKb * 1024L)
    }

    private fun parseKb(line: String): Long? {
        val parts = line.split(Regex("\\s+"))
        if (parts.size < 2) return null
        return parts[1].toLongOrNull()
    }

    private fun runAdbOutput(arguments: List<String>): String {
        return try {
            val process = ProcessBuilder(listOf("adb") + arguments).redirectErrorStream(false).start()
            val output = process.inputStream.bufferedReader().readText()
            process.waitFor()
            output
        } catch (cause: Exception) {
            ""
        }
    }
}
