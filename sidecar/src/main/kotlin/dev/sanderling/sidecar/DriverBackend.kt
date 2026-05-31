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

    // snapshot captures hierarchy then screenshot back-to-back. The service
    // layer holds a mutex around the call so concurrent callers observe a
    // serialized pair from the same on-device frame. Backends may override
    // to fuse the two reads more tightly when their native API allows.
    fun snapshot(): SnapshotSample = SnapshotSample(hierarchy(), screenshot())
}

data class SnapshotSample(
    val hierarchyJson: String,
    val screenshot: Triple<ByteArray, Int, Int>,
)

// STABILITY_POLL_INTERVAL_MILLIS is set wide enough that UiAutomation /
// Maestro's contentDescriptor doesn't get hammered: tighter intervals were
// observed to back the sidecar gRPC stream up under fuzz load to the point
// of triggering 120s inputText deadlines.
internal const val STABILITY_POLL_INTERVAL_MILLIS = 250L
internal const val STABILITY_POLL_CAP_MILLIS = 2000L

// MIN_STABLE_STREAK_MILLIS is the duration the tree must stay structurally
// identical (and non-transitional) before the poll declares settle. Actions
// whose effect is async (e.g. tap-submit fires a DB write that later calls
// popBackStack) leave the UI momentarily stable before the navigation
// transition fires; requiring an uninterrupted streak of this length means
// any churn that starts during the window resets the clock instead of being
// missed.
internal const val MIN_STABLE_STREAK_MILLIS = 750L

// pollUntilStable returns when the snapshot has been non-null and equal to
// itself for an uninterrupted stretch of at least MIN_STABLE_STREAK_MILLIS,
// capped at timeoutMillis. snapshot must omit transient attributes (e.g.
// measure-pass bounds) so layout-only flicker doesn't extend the wait, and
// must return null when the snapshot looks transitional (e.g. mid NavHost
// cross-fade) so the streak resets and the loop keeps polling instead of
// declaring a partial state stable.
internal fun pollUntilStable(timeoutMillis: Long, snapshot: () -> String?) {
    if (timeoutMillis <= 0) return
    val deadline = System.currentTimeMillis() + timeoutMillis
    var prior = try {
        snapshot()
    } catch (_: Exception) {
        null
    }
    var streakStart = 0L
    while (System.currentTimeMillis() < deadline) {
        Thread.sleep(STABILITY_POLL_INTERVAL_MILLIS)
        val current = try {
            snapshot()
        } catch (_: Exception) {
            null
        }
        val now = System.currentTimeMillis()
        if (prior != null && current != null && prior == current) {
            if (streakStart == 0L) streakStart = now
            if (now - streakStart >= MIN_STABLE_STREAK_MILLIS) return
        } else {
            streakStart = 0L
        }
        prior = current
    }
}

// stabilitySnapshot probes the current hierarchy for both structural shape
// and route-transition state. Returns null while more than one route-level
// destination tag (resource-id/testTag/identifier ending in "Screen") is
// present, because NavHost keeps both source and destination composables
// alive during a cross-fade and a snapshot taken in that window is
// unreliable (lazy lists in the incoming screen mount over multiple frames).
internal fun stabilitySnapshot(treeJson: String): String? {
    if (treeJson.isBlank()) return ""
    if (countRouteScreens(treeJson) > 1) return null
    return structuralHash(treeJson)
}

private val ROUTE_TAG_KEYS = setOf(
    "resource-id", "resourceId", "testTag",
    "identifier", "accessibilityIdentifier",
)

internal fun countRouteScreens(treeJson: String): Int {
    if (treeJson.isBlank()) return 0
    return try {
        val mapper = com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
        val root = mapper.readTree(treeJson)
        countRouteScreens(root)
    } catch (_: Exception) {
        0
    }
}

private fun countRouteScreens(node: com.fasterxml.jackson.databind.JsonNode): Int {
    var count = 0
    val attributes = node.get("attributes")
    if (attributes != null && attributes.isObject) {
        for (key in ROUTE_TAG_KEYS) {
            val value = attributes.get(key) ?: continue
            if (value.isNull) continue
            if (value.asText().endsWith("Screen")) {
                count++
                break
            }
        }
    }
    val children = node.get("children")
    if (children != null && children.isArray) {
        for (child in children) count += countRouteScreens(child)
    }
    return count
}

// structuralHash hashes a Maestro TreeNode-shaped JSON string by walking it
// and concatenating only the stable identity attributes (resource-id, class,
// content-desc, text), in tree order. The transient `bounds` and per-frame
// layout coordinates are excluded so a measure-pass that shifts pixels
// without changing what's on screen does not extend the wait.
internal fun structuralHash(treeJson: String): String {
    if (treeJson.isBlank()) return ""
    return try {
        val mapper = com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
        val root = mapper.readTree(treeJson)
        val builder = StringBuilder()
        walkForStructuralHash(root, builder)
        builder.toString()
    } catch (_: Exception) {
        treeJson
    }
}

private val STABLE_ATTRIBUTE_KEYS = listOf(
    "resource-id", "resourceId",
    "class", "className",
    "content-desc", "contentDescription", "accessibilityText",
    "text",
    "testTag", "identifier", "accessibilityIdentifier",
)

private fun walkForStructuralHash(node: com.fasterxml.jackson.databind.JsonNode, out: StringBuilder) {
    out.append('(')
    val attributes = node.get("attributes")
    if (attributes != null && attributes.isObject) {
        for (key in STABLE_ATTRIBUTE_KEYS) {
            val value = attributes.get(key) ?: continue
            if (value.isNull) continue
            out.append(key).append(':').append(value.asText()).append('|')
        }
    }
    val children = node.get("children")
    if (children != null && children.isArray) {
        for (child in children) walkForStructuralHash(child, out)
    }
    out.append(')')
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

private fun execAdb(arguments: List<String>) {
    try {
        val command = ProcessBuilder(listOf("adb") + arguments).redirectErrorStream(true).start()
        command.inputStream.bufferedReader().readText()
        command.waitFor()
    } catch (cause: Exception) {
        println("adb ${arguments.joinToString(" ")} failed: $cause")
    }
}

class StubDriverBackend(
    private val platform: String,
    private val commandRunner: (List<String>) -> Unit = ::execAdb,
) : DriverBackend {
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
        runAdb(listOf("shell", "input", "text", escapeForAdbInputText(text)))
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

    private fun runAdb(arguments: List<String>) = commandRunner(arguments)

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
        // Two-stage settle: an mAnimating poll catches View-system animations,
        // then a short structural-hash poll catches Compose cross-fades where
        // two composables are simultaneously alive but mAnimating is already
        // false. The structural poll is hard-capped so we don't hammer the
        // device with repeat hierarchy fetches when the UI never stabilizes.
        if (durationMillis <= 0) return
        val animationDeadline = System.currentTimeMillis() + durationMillis
        while (System.currentTimeMillis() < animationDeadline) {
            if (isDeviceIdle()) break
            Thread.sleep(IDLE_POLL_INTERVAL_MILLIS)
        }
        pollUntilStable(STABILITY_POLL_CAP_MILLIS) { stabilitySnapshot(hierarchy()) }
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
        val hostPort = java.net.ServerSocket(0).use { it.localPort }
        driver = maestro.drivers.AndroidDriver(dadb, hostPort)
        driver.open()
    }

    override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) {
        if (clearState) driver.clearAppState(bundleId)
        driver.launchApp(bundleId, env)
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
        // waitForAppToSettle returns early on Compose cross-fade transitions
        // where both source and destination composables are semantically
        // alive; a follow-up short structural-hash poll lands on a single
        // stable frame before the runner reads hierarchy + screenshot
        // concurrently. The structural poll is hard-capped independently of
        // durationMillis so we don't pile on hierarchy fetches when settle
        // never converges.
        driver.waitForAppToSettle(null, null, durationMillis.toInt())
        pollUntilStable(STABILITY_POLL_CAP_MILLIS) {
            stabilitySnapshot(
                com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
                    .writeValueAsString(driver.contentDescriptor(false)),
            )
        }
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
    private lateinit var driver: maestro.drivers.IOSDriver
    private val reconnectLock = java.util.concurrent.locks.ReentrantLock()

    init {
        val wdaPort = maestro.utils.SocketUtils.nextFreePort(22000, 23000)
        val tempFileHandler = maestro.utils.TempFileHandler()
        val simctlDevice = device.SimctlIOSDevice(
            deviceId = udid,
            tempFileHandler = tempFileHandler,
        )
        val driverConfig = xcuitest.installer.LocalXCTestInstaller.IOSDriverConfig(
            prebuiltRunner = false,
            sourceDirectory = "driver-iPhoneSimulator",
            context = xcuitest.installer.Context.CLI,
            snapshotKeyHonorModalViews = null,
        )
        val installer = xcuitest.installer.LocalXCTestInstaller(
            deviceId = udid,
            host = "127.0.0.1",
            deviceType = util.IOSDeviceType.SIMULATOR,
            defaultPort = wdaPort,
            iOSDriverConfig = driverConfig,
            deviceController = simctlDevice,
            tempFileHandler = tempFileHandler,
        )
        val xcTestClient = xcuitest.XCTestClient("127.0.0.1", wdaPort)
        val xcTestDriverClient = xcuitest.XCTestDriverClient(
            installer = installer,
            client = xcTestClient,
        )
        val xcRunnerUtils = util.XCRunnerCLIUtils(tempFileHandler)
        val xcTestDevice = ios.xctest.XCTestIOSDevice(
            deviceId = udid,
            client = xcTestDriverClient,
            getInstalledApps = { xcRunnerUtils.listApps(udid) },
        )
        val device = ios.LocalIOSDevice(
            deviceId = udid,
            xcTestDevice = xcTestDevice,
            deviceController = simctlDevice,
            insights = maestro.utils.NoopInsights,
        )
        driver = maestro.drivers.IOSDriver(device, maestro.utils.NoopInsights)
        driver.open()
        warmup()
    }

    private fun warmup() {
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

    private fun <T> withReconnect(block: () -> T): T {
        return try {
            block()
        } catch (e: Exception) {
            val isIoFailure = generateSequence(e as Throwable) { it.cause }
                .any { it is java.io.IOException }
            if (!isIoFailure) throw e
            reconnectLock.lock()
            try {
                try { driver.open(); warmup() }
                catch (reconnectErr: Exception) {
                    throw IllegalStateException("WDA reconnect failed: $reconnectErr", e)
                }
            } finally {
                reconnectLock.unlock()
            }
            block()
        }
    }

    override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) = withReconnect {
        runCatching { driver.stopApp(bundleId) }
        if (clearState) driver.clearAppState(bundleId)
        driver.launchApp(bundleId, env)
    }

    override fun terminate(bundleId: String) = withReconnect { driver.stopApp(bundleId) }

    override fun tap(x: Int, y: Int) = withReconnect { driver.tap(maestro.Point(x, y)) }

    override fun tapSelector(selector: String) = withReconnect {
        val root = driver.contentDescriptor(false)
        val bounds = findBoundsBySelector(root, selector) ?: return@withReconnect
        driver.tap(maestro.Point((bounds[0] + bounds[2]) / 2, (bounds[1] + bounds[3]) / 2))
    }

    override fun inputText(text: String) = withReconnect { driver.inputText(text) }

    override fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long) = withReconnect {
        driver.swipe(maestro.Point(fromX, fromY), maestro.Point(toX, toY), maxOf(durationMillis, 250L))
    }

    override fun pressKey(key: String) = withReconnect {
        StubDriverBackend.KEY_MAP[key]?.let { keyCode ->
            keyCodeToMaestro(keyCode)?.let { driver.pressKey(it) }
        }
        Unit
    }

    override fun screenshot(): Triple<ByteArray, Int, Int> = withReconnect {
        val buf = okio.Buffer()
        driver.takeScreenshot(buf, false)
        val bytes = buf.readByteArray()
        Triple(bytes, pngWidth(bytes), pngHeight(bytes))
    }

    override fun hierarchy(): String = withReconnect {
        com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
            .writeValueAsString(driver.contentDescriptor(false))
    }

    override fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine> = emptyList()

    override fun waitForIdle(durationMillis: Long) = withReconnect {
        driver.waitForAppToSettle(null, null, durationMillis.toInt())
        pollUntilStable(STABILITY_POLL_CAP_MILLIS) {
            stabilitySnapshot(
                com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
                    .writeValueAsString(driver.contentDescriptor(false)),
            )
        }
        Unit
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
