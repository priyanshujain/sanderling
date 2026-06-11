package dev.sanderling.sidecar

interface DriverBackend {
    fun launch(bundleId: String, clearState: Boolean, env: Map<String, String> = emptyMap())
    fun terminate(bundleId: String)
    fun tap(x: Int, y: Int)

    // doubleTap lands two taps as close together as the platform allows.
    // The default composes two taps back-to-back; backends with higher
    // per-tap latency override to tighten the gap.
    fun doubleTap(x: Int, y: Int) {
        tap(x, y)
        tap(x, y)
    }

    fun tapSelector(selector: String)
    fun inputText(text: String)
    fun eraseText(characterCount: Int)
    fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long)
    fun pressKey(key: String)
    fun longPress(x: Int, y: Int)
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

    // close releases device-side resources on shutdown. The iOS backend must
    // stop its XCTest runner here: an orphaned runner session auto-restarts
    // later and hijacks the simulator's gesture daemon mid-run.
    fun close() {}
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

// overlappedDoubleTap fires the second tap while the first is still in
// flight, so the on-device gap stays tight on transports with high per-tap
// latency. The overlap can collide with the other tap still executing ("only
// one gesture can be performed at a time") on either leg; the colliding leg
// then lands sequentially after the surviving one instead of failing the
// step.
internal fun overlappedDoubleTap(tapAction: () -> Unit) {
    val firstTap = java.util.concurrent.CompletableFuture.runAsync { tapAction() }
    Thread.sleep(40)
    try {
        tapAction()
    } catch (_: Throwable) {
        runCatching { firstTap.join() }
        tapAction()
        return
    }
    try {
        firstTap.join()
    } catch (_: Throwable) {
        tapAction()
    }
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

internal fun parseCpuTicks(statLine: String): Long? {
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

internal fun parseKb(line: String): Long? {
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

    @Volatile var lastEraseCharacterCount: Int? = null
        private set

    override fun eraseText(characterCount: Int) {
        lastEraseCharacterCount = characterCount
        repeat(characterCount) {
            runAdb(listOf("shell", "input", "keyevent", "KEYCODE_DEL"))
        }
    }

    @Volatile var lastSwipe: SwipeRecord? = null
        private set
    @Volatile var lastKey: String? = null
        private set
    @Volatile var lastLongPress: Pair<Int, Int>? = null
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

    override fun longPress(x: Int, y: Int) {
        lastLongPress = x to y
        runAdb(listOf("shell", "input", "swipe", x.toString(), y.toString(), x.toString(), y.toString(), "600"))
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

// FAST_INPUT_SAFE matches text that can be typed with adb `input text`: ASCII,
// free of shell metacharacters and spaces, regardless of length. Anything else
// (unicode, injection payloads, whitespace) falls back to the driver path. The
// first character excludes '-' so the text can never be read as an option by
// `input text`. Length is unbounded on purpose: the slow per-character driver
// path takes ~120s for a 4096-char string (blowing the RPC deadline) and leaves
// focus unguarded long enough to spray keystrokes into the launcher search box
// if the app loses the foreground mid-type; the shell path types in chunks with
// a foreground re-check between them (see typeShellSafe).
internal val FAST_INPUT_SAFE = Regex("^[A-Za-z0-9@._+][A-Za-z0-9@._+-]*$")

// INPUT_CHUNK_CHARS bounds each `input text` shell invocation so a long string
// is typed as a series of short, interruptible commands rather than one opaque
// ~18s call. Small enough that a foreground re-check between chunks catches a
// focus escape early; large enough that the per-chunk dumpsys cost stays minor.
internal const val INPUT_CHUNK_CHARS = 512

// chunkForInput splits text into pieces of at most `size` characters, never
// ending a piece right before a '-': a chunk that began with '-' would be read
// as an option by `input text`. The whole string's first character is already
// guaranteed non-'-' by FAST_INPUT_SAFE, so the first chunk is always safe too.
internal fun chunkForInput(text: String, size: Int): List<String> {
    require(size > 0)
    val chunks = mutableListOf<String>()
    var start = 0
    while (start < text.length) {
        var end = minOf(start + size, text.length)
        while (end < text.length && text[end] == '-') end++
        chunks.add(text.substring(start, end))
        start = end
    }
    return chunks
}

class MaestroDriverBackend(private val serial: String?) : DriverBackend {
    private val dadb: dadb.Dadb
    private val driver: maestro.drivers.AndroidDriver

    init {
        dadb = buildDadb(serial)
        val hostPort = java.net.ServerSocket(0).use { it.localPort }
        driver = maestro.drivers.AndroidDriver(dadb, hostPort)
        openWithRetry()
    }

    // openWithRetry tolerates the maestro Android driver's occasional startup
    // timeout (its instrumentation host can miss the dadb.open() deadline,
    // especially right after a device reboot or per-run reinstall). A transient
    // failure should not abort the whole run, so retry a few times with a short
    // backoff before giving up.
    private fun openWithRetry() {
        val attempts = 4
        for (attempt in 1..attempts) {
            try {
                driver.open()
                return
            } catch (cause: Exception) {
                runCatching { driver.close() }
                if (attempt == attempts) throw cause
                System.err.println("android driver open failed (attempt $attempt/$attempts): ${cause.message}; retrying")
                Thread.sleep(2000)
            }
        }
    }

    override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) {
        if (clearState) driver.clearAppState(bundleId)
        driver.launchApp(bundleId, env)
    }

    override fun terminate(bundleId: String) = driver.stopApp(bundleId)

    override fun tap(x: Int, y: Int) = driver.tap(maestro.Point(x, y))

    override fun longPress(x: Int, y: Int) = driver.longPress(maestro.Point(x, y))

    override fun tapSelector(selector: String) {
        val root = driver.contentDescriptor(false)
        val bounds = findBoundsBySelector(root, selector) ?: return
        driver.tap(maestro.Point((bounds[0] + bounds[2]) / 2, (bounds[1] + bounds[3]) / 2))
    }

    override fun inputText(text: String) {
        if (FAST_INPUT_SAFE.matches(text)) {
            // adb `input text` is far faster than the driver's per-character
            // path. Restricted to shell-safe ASCII so unicode and injection
            // payloads still go through the driver, which handles them. The
            // runner focuses the field with a tap before InputText, so the
            // keystrokes land in it.
            typeShellSafe(text)
        } else {
            driver.inputText(text)
        }
    }

    // typeShellSafe types shell-safe ASCII through adb `input text` in chunks,
    // re-checking the foreground app before each chunk. If the app the type
    // started in has lost the foreground, the remaining keystrokes would spray
    // into whatever window stole it (the launcher search box, in practice), so
    // typing stops instead of leaking out of the app under test.
    private fun typeShellSafe(text: String) {
        val owner = foregroundPackage()
        var typed = 0
        for (chunk in chunkForInput(text, INPUT_CHUNK_CHARS)) {
            if (owner != null && typed > 0 && foregroundPackage() != owner) {
                System.err.println(
                    "warn: inputText stopped; foreground left $owner mid-type after $typed/${text.length} chars",
                )
                return
            }
            dadb.shell("input text $chunk")
            typed += chunk.length
        }
    }

    // foregroundPackage returns the package of the top resumed activity, or null
    // if it cannot be read. Used to detect mid-type focus escapes.
    private fun foregroundPackage(): String? {
        val output = adbOutput(serial, listOf("shell", "dumpsys", "activity", "activities"))
        return Regex("""topResumedActivity=ActivityRecord\{\S+ \S+ ([^/\s]+)/""")
            .find(output)?.groupValues?.get(1)
    }

    override fun eraseText(characterCount: Int) = driver.eraseText(characterCount)

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
        // waitForAppToSettle blocks on the View-system animation and maestro's
        // own structural settle, which is enough on its own. A follow-up
        // structural-hash poll used to run here, but each hierarchy fetch is
        // ~500ms on a physical device, so it cost ~2.8s per mutating step for
        // marginal benefit; the runner already re-fetches while a frame still
        // looks transitional.
        driver.waitForAppToSettle(null, null, durationMillis.toInt())
    }

    override fun healthy() = runCatching { driver.contentDescriptor(false); true }.getOrElse { false }

    override fun metrics(bundleId: String) = readProcMetrics(serial, bundleId)

    override fun close() {
        runCatching { driver.close() }
        runCatching { dadb.close() }
    }
}

internal sealed interface DadbTarget {
    data class Tcp(val host: String, val port: Int) : DadbTarget
    data class Server(val serial: String) : DadbTarget
}

// A host:port serial connects to adbd directly; any other serial is reached
// through the adb server, the only path to a USB-attached device. A null serial
// keeps the emulator loopback default.
internal fun dadbTargetFor(serial: String?): DadbTarget {
    if (serial == null) return DadbTarget.Tcp("localhost", 5555)
    val colon = serial.lastIndexOf(':')
    val port = if (colon >= 0) serial.substring(colon + 1).toIntOrNull() else null
    return if (port != null) DadbTarget.Tcp(serial.substring(0, colon), port) else DadbTarget.Server(serial)
}

private fun buildDadb(serial: String?): dadb.Dadb = when (val target = dadbTargetFor(serial)) {
    is DadbTarget.Tcp -> dadb.Dadb.create(target.host, target.port)
    is DadbTarget.Server -> dadb.adbserver.AdbServer.createDadb("localhost", 5037, "host:transport:${target.serial}")
}

internal fun findBoundsBySelector(root: maestro.TreeNode, selector: String): IntArray? {
    val colon = selector.indexOf(':')
    if (colon < 0) return null
    val kind = selector.substring(0, colon)
    val value = selector.substring(colon + 1)
    return findBoundsInTree(root, kind, value)
}

internal fun findBoundsInTree(node: maestro.TreeNode, kind: String, value: String): IntArray? {
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

internal fun parseBounds(s: String): IntArray? {
    val pattern = Regex("^\\[(-?\\d+),(-?\\d+),(-?\\d+),(-?\\d+)\\]$")
    val m = pattern.matchEntire(s) ?: return null
    return IntArray(4) { m.groupValues[it + 1].toInt() }
}

internal fun pngWidth(bytes: ByteArray): Int {
    if (bytes.size < 24) return 0
    return (bytes[16].toInt() and 0xFF shl 24) or (bytes[17].toInt() and 0xFF shl 16) or
        (bytes[18].toInt() and 0xFF shl 8) or (bytes[19].toInt() and 0xFF)
}

internal fun pngHeight(bytes: ByteArray): Int {
    if (bytes.size < 24) return 0
    return (bytes[20].toInt() and 0xFF shl 24) or (bytes[21].toInt() and 0xFF shl 16) or
        (bytes[22].toInt() and 0xFF shl 8) or (bytes[23].toInt() and 0xFF)
}

internal const val IOS_XCTEST_RUNNER_BUNDLE_ID = "dev.mobile.maestro-driver-iosUITests.xctrunner"

// reapOrphanIosRunners kills XCTest runner sessions left over from a prior
// run. A sidecar that died without its shutdown hook leaves its xcodebuild
// session alive; xcodebuild later restarts its dead runner, which terminates
// the active run's session and steals the simulator's gesture daemon. Returns
// true when an orphaned xcodebuild session was found and killed.
internal fun reapOrphanIosRunners(udid: String, execute: (List<String>) -> Int): Boolean {
    val killed = execute(listOf("pkill", "-f", "xcodebuild.*test-without-building.*$udid")) == 0
    execute(listOf("xcrun", "simctl", "terminate", udid, IOS_XCTEST_RUNNER_BUNDLE_ID))
    return killed
}

// WdaRecovery serializes XCTest runner recovery across concurrent RPCs. An
// IOException on one call does not prove the runner is down (an overlapped
// gesture can reset a single connection), and a full runner restart costs
// around 50 seconds of downtime, so recovery probes channel liveness first
// and only restarts a dead channel. The probe re-runs under the lock so
// threads queued behind an in-flight restart do not restart again.
internal class WdaRecovery(
    private val isAlive: () -> Boolean,
    private val restart: () -> Unit,
    private val log: (String) -> Unit = ::println,
) {
    private val lock = java.util.concurrent.locks.ReentrantLock()

    // run executes block, recovering the channel on IO failure. replay re-runs
    // the block afterwards and is only safe for idempotent reads: an action
    // can fail client-side after the device already applied it, so replaying
    // types text or taps twice. Non-idempotent actions surface UNAVAILABLE,
    // which the runner treats as transient.
    fun <T> run(replay: Boolean, block: () -> T): T {
        return try {
            block()
        } catch (e: Exception) {
            if (!isIoFailure(e)) throw e
            recover(e)
            if (!replay) {
                throw io.grpc.Status.UNAVAILABLE
                    .withDescription("connection dropped mid-action; the action may have applied: ${e.message}")
                    .withCause(e).asRuntimeException()
            }
            try {
                block()
            } catch (retryErr: Exception) {
                if (!isIoFailure(retryErr)) throw retryErr
                throw io.grpc.Status.UNAVAILABLE
                    .withDescription("read retry failed after channel recovery: ${retryErr.message}")
                    .withCause(retryErr).asRuntimeException()
            }
        }
    }

    private fun isIoFailure(e: Exception): Boolean =
        generateSequence(e as Throwable) { it.cause }.any { it is java.io.IOException }

    private fun recover(cause: Exception) {
        lock.lock()
        try {
            if (isAlive()) {
                log("channel alive after $cause; skipping runner restart")
                return
            }
            log("channel dead after $cause; restarting the XCTest runner")
            val startedAt = System.currentTimeMillis()
            try {
                restart()
            } catch (restartErr: Exception) {
                throw IllegalStateException("WDA reconnect failed: $restartErr", cause)
            }
            log("XCTest runner restarted in ${System.currentTimeMillis() - startedAt} ms")
        } finally {
            lock.unlock()
        }
    }
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
