package dev.sanderling.sidecar

interface DriverBackend {
    fun launch(bundleId: String, clearState: Boolean, env: Map<String, String> = emptyMap())
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

internal fun readLogcat(serial: String?, sinceUnixMillis: Long, minLevel: String): List<LogLine> {
    val level = if (minLevel.isEmpty()) "E" else minLevel
    val since = if (sinceUnixMillis > 0) StubDriverBackend.formatAdbLogcatTimestamp(sinceUnixMillis) else null
    val arguments = mutableListOf("logcat", "-d", "*:$level")
    if (since != null) {
        arguments.add("-T")
        arguments.add(since)
    }
    return try {
        val process = ProcessBuilder(adbCmd(serial) + arguments).redirectErrorStream(false).start()
        val output = process.inputStream.bufferedReader().readText()
        process.waitFor()
        StubDriverBackend.parseLogcatOutput(output)
    } catch (cause: Exception) {
        println("adb logcat failed: $cause")
        emptyList()
    }
}

internal fun readProcMetrics(serial: String?, bundleId: String): MetricsSample {
    if (bundleId.isEmpty()) return MetricsSample(0.0, 0L, 0L)
    return try {
        val pid = adbOutput(serial, listOf("shell", "pidof", bundleId))
            .trim().split(Regex("\\s+")).firstOrNull()?.toIntOrNull()
            ?: return MetricsSample(0.0, 0L, 0L)
        val cpu = sampleCpuTwice(serial, pid)
        val (rssBytes, vmSizeBytes) = sampleProcessMemory(serial, pid)
        MetricsSample(cpu, rssBytes, vmSizeBytes)
    } catch (cause: Exception) {
        println("metrics capture failed: $cause")
        MetricsSample(0.0, 0L, 0L)
    }
}

private fun adbCmd(serial: String?): List<String> =
    if (serial == null) listOf("adb") else listOf("adb", "-s", serial)

private fun adbOutput(serial: String?, arguments: List<String>): String {
    return try {
        val process = ProcessBuilder(adbCmd(serial) + arguments).redirectErrorStream(false).start()
        val output = process.inputStream.bufferedReader().readText()
        process.waitFor()
        output
    } catch (cause: Exception) {
        ""
    }
}

private fun sampleCpuTwice(serial: String?, pid: Int): Double {
    val sleepArg = "0.050"
    val command = "cat /proc/$pid/stat; sleep $sleepArg; cat /proc/$pid/stat"
    val output = adbOutput(serial, listOf("shell", command))
    val lines = output.lines().filter { it.isNotBlank() }
    if (lines.size < 2) return 0.0
    val first = parseCpuTicks(lines[0]) ?: return 0.0
    val second = parseCpuTicks(lines[1]) ?: return 0.0
    val clockHz = adbOutput(serial, listOf("shell", "getconf", "CLK_TCK")).trim().toLongOrNull() ?: 100L
    val deltaCpuNanos = (second - first) * 1_000_000_000.0 / clockHz.coerceAtLeast(1L)
    return (deltaCpuNanos / 50_000_000.0) * 100.0
}

private fun parseCpuTicks(statLine: String): Long? {
    val afterComm = statLine.substringAfterLast(')').trim()
    val fields = afterComm.split(Regex("\\s+"))
    if (fields.size < 13) return null
    val utime = fields[11].toLongOrNull() ?: return null
    val stime = fields[12].toLongOrNull() ?: return null
    return utime + stime
}

private fun sampleProcessMemory(serial: String?, pid: Int): Pair<Long, Long> {
    val status = adbOutput(serial, listOf("shell", "cat", "/proc/$pid/status"))
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

    override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) {
        launchCount++
        lastBundleId = bundleId
        if (clearState) {
            runAdb(listOf("shell", "pm", "clear", bundleId))
        }
        runAdb(listOf("shell", "am", "start", "-W", "-n", "$bundleId/.MainActivity"))
    }

    companion object {
        private const val IDLE_POLL_INTERVAL_MILLIS = 50L

        internal fun isAnimationCountIdle(grepOutput: String): Boolean =
            (grepOutput.trim().toIntOrNull() ?: 0) == 0

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

        internal const val MAX_CLEAR_DELETES: Int = 1024

        internal fun buildClearKeyevents(textLength: Int): List<String> {
            if (textLength <= 0) return emptyList()
            val deletes = minOf(textLength, MAX_CLEAR_DELETES)
            val args = mutableListOf("shell", "input", "keyevent", "KEYCODE_MOVE_END")
            repeat(deletes) { args.add("KEYCODE_DEL") }
            return args
        }

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
    }

    override fun inputText(text: String) {
        lastInputText = text
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

    override fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine> =
        readLogcat(null, sinceUnixMillis, minLevel)

    data class SwipeRecord(val fromX: Int, val fromY: Int, val toX: Int, val toY: Int, val durationMillis: Long)

    private fun runAdb(arguments: List<String>) {
        try {
            val command = ProcessBuilder(listOf("adb") + arguments).redirectErrorStream(true).start()
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
                    "adb", "exec-out",
                    "uiautomator dump /data/local/tmp/window_dump.xml >/dev/null 2>&1 && cat /data/local/tmp/window_dump.xml",
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
        if (durationMillis <= 0) return
        val deadline = System.currentTimeMillis() + durationMillis
        while (System.currentTimeMillis() < deadline) {
            if (isDeviceIdle()) return
            Thread.sleep(IDLE_POLL_INTERVAL_MILLIS)
        }
    }

    private fun isDeviceIdle(): Boolean {
        return try {
            val output = adbOutput(null, listOf("shell", "dumpsys window -a | grep -c mAnimating=true"))
            isAnimationCountIdle(output)
        } catch (cause: Exception) {
            false
        }
    }

    override fun healthy(): Boolean = true

    override fun metrics(bundleId: String): MetricsSample = readProcMetrics(null, bundleId)
}

class MaestroDriverBackend(private val serial: String?) : DriverBackend {
    private val dadb: dadb.Dadb
    private val driver: maestro.drivers.AndroidDriver

    init {
        dadb = buildDadb(serial)
        driver = maestro.drivers.AndroidDriver(dadb, 7001, "localhost")
        driver.open()
    }

    override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) {
        if (clearState) driver.clearAppState(bundleId)
        driver.launchApp(bundleId, env, java.util.UUID.randomUUID())
    }

    override fun terminate(bundleId: String) = driver.stopApp(bundleId)

    override fun tap(x: Int, y: Int) = driver.tap(maestro.Point(x, y))

    override fun tapSelector(selector: String) {
        val root = driver.contentDescriptor(false)
        val bounds = findBoundsBySelector(root, selector) ?: return
        driver.tap(maestro.Point((bounds[0] + bounds[2]) / 2, (bounds[1] + bounds[3]) / 2))
    }

    override fun inputText(text: String) = driver.inputText(text)

    override fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long) =
        driver.swipe(maestro.Point(fromX, fromY), maestro.Point(toX, toY), maxOf(durationMillis, 250L))

    override fun pressKey(key: String) {
        StubDriverBackend.KEY_MAP[key]?.let { keyCode ->
            keyCodeToMaestro(keyCode)?.let { driver.pressKey(it) }
        }
    }

    override fun screenshot(): Triple<ByteArray, Int, Int> {
        val buf = okio.Buffer()
        driver.takeScreenshot(buf, false)
        val bytes = buf.readByteArray()
        return Triple(bytes, pngWidth(bytes), pngHeight(bytes))
    }

    override fun hierarchy(): String =
        com.fasterxml.jackson.module.kotlin.jacksonObjectMapper().writeValueAsString(driver.contentDescriptor(false))

    override fun recentLogs(sinceUnixMillis: Long, minLevel: String) =
        readLogcat(serial, sinceUnixMillis, minLevel)

    override fun waitForIdle(durationMillis: Long) {
        driver.waitForAppToSettle(null, null, durationMillis.toInt())
    }

    override fun healthy() = runCatching { driver.contentDescriptor(false); true }.getOrElse { false }

    override fun metrics(bundleId: String) = readProcMetrics(serial, bundleId)
}

private fun buildDadb(serial: String?): dadb.Dadb {
    return if (serial == null) {
        dadb.Dadb.create("localhost", 5555)
    } else {
        dadb.Dadb.create(serial.substringBefore(":"), serial.substringAfter(":").toIntOrNull() ?: 5555)
    }
}

private fun findBoundsBySelector(root: maestro.TreeNode, selector: String): IntArray? {
    val colon = selector.indexOf(':')
    if (colon < 0) return null
    val kind = selector.substring(0, colon)
    val value = selector.substring(colon + 1)
    return findBoundsInTree(root, kind, value)
}

private fun findBoundsInTree(node: maestro.TreeNode, kind: String, value: String): IntArray? {
    val attrs = node.attributes
    val matches = when (kind) {
        "id" -> attrs["resource-id"]?.let { it == value || it.endsWith(":id/$value") } == true
        "text" -> attrs["text"] == value
        "desc" -> attrs["content-desc"] == value
        "descPrefix" -> attrs["content-desc"]?.startsWith(value) == true
        else -> false
    }
    if (matches) {
        attrs["bounds"]?.let { b -> parseBounds(b)?.let { return it } }
    }
    for (child in node.children) {
        findBoundsInTree(child, kind, value)?.let { return it }
    }
    return null
}

private fun parseBounds(s: String): IntArray? {
    val pattern = Regex("^\\[(-?\\d+),(-?\\d+),(-?\\d+),(-?\\d+)\\]$")
    val m = pattern.matchEntire(s) ?: return null
    return IntArray(4) { m.groupValues[it + 1].toInt() }
}

private fun pngWidth(bytes: ByteArray): Int {
    if (bytes.size < 24) return 0
    return (bytes[16].toInt() and 0xFF shl 24) or (bytes[17].toInt() and 0xFF shl 16) or
        (bytes[18].toInt() and 0xFF shl 8) or (bytes[19].toInt() and 0xFF)
}

private fun pngHeight(bytes: ByteArray): Int {
    if (bytes.size < 24) return 0
    return (bytes[20].toInt() and 0xFF shl 24) or (bytes[21].toInt() and 0xFF shl 16) or
        (bytes[22].toInt() and 0xFF shl 8) or (bytes[23].toInt() and 0xFF)
}

class IosDriverBackend(private val udid: String) : DriverBackend {
    private val driver: maestro.drivers.IOSDriver

    init {
        val httpClient = xcuitest.api.OkHttpClientInstance.get()
        val metrics = maestro.utils.NoOpMetrics()
        val wdaPort = maestro.utils.SocketUtils.nextFreePort(22000, 23000)
        val installer = xcuitest.installer.LocalXCTestInstaller(
            udid,
            "localhost",
            false,
            wdaPort,
            metrics,
            httpClient,
            false,
            false,
        )
        val xcTestDriverClient = xcuitest.XCTestDriverClient(installer, httpClient, false)
        val xcTestDevice = ios.xctest.XCTestIOSDevice(udid, xcTestDriverClient) { emptySet() }
        val simctlDevice = ios.simctl.SimctlIOSDevice(udid)
        val device = ios.LocalIOSDevice(udid, xcTestDevice, simctlDevice, maestro.utils.NoopInsights)
        driver = maestro.drivers.IOSDriver(device, maestro.utils.NoopInsights, metrics)
        driver.open()
        // warm up: absorbs WDA startup race (health check passes before accept() loop is stable)
        var warmupErr: Exception? = null
        repeat(3) { attempt ->
            try {
                driver.contentDescriptor(false)
                warmupErr = null
                return@repeat
            } catch (e: Exception) {
                warmupErr = e
                if (attempt < 2) Thread.sleep(500)
            }
        }
        warmupErr?.let { throw IllegalStateException("WDA warmup failed after 3 attempts: $it") }
    }

    override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) {
        runCatching { driver.stopApp(bundleId) }
        if (clearState) driver.clearAppState(bundleId)
        driver.launchApp(bundleId, env, java.util.UUID.randomUUID())
    }

    override fun terminate(bundleId: String) = driver.stopApp(bundleId)

    override fun tap(x: Int, y: Int) = driver.tap(maestro.Point(x, y))

    override fun tapSelector(selector: String) {
        val root = driver.contentDescriptor(false)
        val bounds = findBoundsBySelector(root, selector) ?: return
        driver.tap(maestro.Point((bounds[0] + bounds[2]) / 2, (bounds[1] + bounds[3]) / 2))
    }

    override fun inputText(text: String) = driver.inputText(text)

    override fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long) =
        driver.swipe(maestro.Point(fromX, fromY), maestro.Point(toX, toY), maxOf(durationMillis, 250L))

    override fun pressKey(key: String) {
        StubDriverBackend.KEY_MAP[key]?.let { keyCode ->
            keyCodeToMaestro(keyCode)?.let { driver.pressKey(it) }
        }
    }

    override fun screenshot(): Triple<ByteArray, Int, Int> {
        val buf = okio.Buffer()
        driver.takeScreenshot(buf, false)
        val bytes = buf.readByteArray()
        return Triple(bytes, pngWidth(bytes), pngHeight(bytes))
    }

    override fun hierarchy(): String =
        com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
            .writeValueAsString(driver.contentDescriptor(false))

    override fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine> = emptyList()

    override fun waitForIdle(durationMillis: Long) {
        driver.waitForAppToSettle(null, null, durationMillis.toInt())
    }

    override fun healthy() = runCatching { driver.contentDescriptor(false); true }.getOrElse { false }

    override fun metrics(bundleId: String) = MetricsSample(0.0, 0L, 0L)
}

private fun keyCodeToMaestro(adbKeyCode: String): maestro.KeyCode? {
    return when (adbKeyCode) {
        "KEYCODE_BACK" -> maestro.KeyCode.BACK
        "KEYCODE_HOME" -> maestro.KeyCode.HOME
        "KEYCODE_ENTER" -> maestro.KeyCode.ENTER
        "KEYCODE_TAB" -> maestro.KeyCode.TAB
        "KEYCODE_DPAD_UP" -> maestro.KeyCode.REMOTE_UP
        "KEYCODE_DPAD_DOWN" -> maestro.KeyCode.REMOTE_DOWN
        "KEYCODE_DPAD_LEFT" -> maestro.KeyCode.REMOTE_LEFT
        "KEYCODE_DPAD_RIGHT" -> maestro.KeyCode.REMOTE_RIGHT
        else -> null
    }
}
