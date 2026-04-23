package app.folio.sanderling

import kotlinx.cinterop.ExperimentalForeignApi
import platform.Foundation.NSBundle
import platform.Foundation.NSThread

@OptIn(ExperimentalForeignApi::class)
internal object IosAgent {
    private const val VERSION = "0.0.1"
    private const val PROTOCOL_VERSION = 1

    fun start(host: String, port: Int) {
        val thread = NSThread { runLoop(host, port) }
        thread.name = "sanderling-agent"
        thread.start()
    }

    private fun runLoop(host: String, port: Int) {
        var delayMs = 0L
        while (true) {
            if (delayMs > 0) NSThread.sleepForTimeInterval(delayMs / 1000.0)
            try {
                val conn = TcpConnection.connect(host, port)
                delayMs = 0L
                try { serve(conn) } finally { conn.close() }
            } catch (_: Exception) {
                delayMs = if (delayMs <= 0L) 500L else minOf(delayMs * 2, 10_000L)
            }
        }
    }

    private fun serve(conn: TcpConnection) {
        val appPackage = (NSBundle.mainBundle.infoDictionary?.get("CFBundleIdentifier") as? String) ?: "unknown"
        conn.writeFrame(
            """{"type":"HELLO","protocol_version":$PROTOCOL_VERSION,"version":${jsonString(VERSION)},"platform":"ios","app_package":${jsonString(appPackage)}}""".encodeToByteArray()
        )
        while (true) {
            val frame = conn.readFrame()
            val text = frame.decodeToString()
            val type = extractJsonField(text, "type") ?: break
            val id = extractJsonLong(text, "id") ?: 0L
            when (type) {
                "PAUSE" -> handlePause(conn, id)
                "RESUME" -> IosPauser.release()
                "GOODBYE" -> return
            }
        }
    }

    private fun handlePause(conn: TcpConnection, id: Long) {
        val snapshots = IosPauser.pauseAndSnapshot {
            val snap = SanderlingIos.extractors.toMap()
            buildMap { for ((name, extractor) in snap) put(name, runCatching { extractor() }.getOrNull()) }
        }
        val snapshotsJson = snapshots.entries.joinToString(",") { (k, v) -> "${jsonString(k)}:${jsonValue(v)}" }
        conn.writeFrame("""{"type":"STATE","id":$id,"snapshots":{$snapshotsJson}}""".encodeToByteArray())
    }
}

internal fun extractJsonField(json: String, key: String): String? =
    Regex("\"${Regex.escape(key)}\"\\s*:\\s*\"([^\"\\\\]*(?:\\\\.[^\"\\\\]*)*)\"").find(json)?.groupValues?.get(1)

internal fun extractJsonLong(json: String, key: String): Long? =
    Regex("\"${Regex.escape(key)}\"\\s*:\\s*(-?\\d+)").find(json)?.groupValues?.get(1)?.toLongOrNull()

internal fun jsonString(s: String): String = buildString {
    append('"')
    for (c in s) when (c) {
        '"' -> append("\\\"")
        '\\' -> append("\\\\")
        '\n' -> append("\\n")
        '\r' -> append("\\r")
        '\t' -> append("\\t")
        else -> if (c.code < 0x20) append("\\u${c.code.toString(16).padStart(4, '0')}") else append(c)
    }
    append('"')
}

internal fun jsonValue(value: Any?): String = when (value) {
    null -> "null"
    is Boolean -> if (value) "true" else "false"
    is Int -> value.toString()
    is Long -> value.toString()
    is Float -> value.toString()
    is Double -> value.toString()
    is String -> jsonString(value)
    is Map<*, *> -> "{${value.entries.joinToString(",") { (k, v) -> "${jsonString(k.toString())}:${jsonValue(v)}" }}}"
    is List<*> -> "[${value.joinToString(",") { jsonValue(it) }}]"
    else -> jsonString(value.toString())
}
