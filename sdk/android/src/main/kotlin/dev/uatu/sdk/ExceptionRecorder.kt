package dev.uatu.sdk

import java.io.PrintWriter
import java.io.StringWriter

internal class ExceptionRecorder(private val capacity: Int = DEFAULT_CAPACITY) {
    data class Entry(
        val className: String,
        val message: String,
        val stackTrace: String,
        val unixMillis: Long,
    )

    private val buffer: ArrayDeque<Entry> = ArrayDeque()
    private var chainedHandler: Thread.UncaughtExceptionHandler? = null
    @Volatile private var installed: Boolean = false

    @Synchronized
    fun install() {
        if (installed) return
        chainedHandler = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, throwable ->
            record(throwable)
            chainedHandler?.uncaughtException(thread, throwable)
        }
        installed = true
    }

    @Synchronized
    fun uninstall() {
        if (!installed) return
        Thread.setDefaultUncaughtExceptionHandler(chainedHandler)
        chainedHandler = null
        installed = false
    }

    @Synchronized
    fun record(throwable: Throwable, now: Long = System.currentTimeMillis()) {
        val stackTrace = StringWriter().also { throwable.printStackTrace(PrintWriter(it)) }.toString()
        val entry = Entry(
            className = throwable.javaClass.name,
            message = throwable.message ?: "",
            stackTrace = stackTrace,
            unixMillis = now,
        )
        if (buffer.size >= capacity) {
            buffer.removeFirst()
        }
        buffer.addLast(entry)
    }

    @Synchronized
    fun drain(): List<Entry> {
        val snapshot = buffer.toList()
        buffer.clear()
        return snapshot
    }

    companion object {
        const val DEFAULT_CAPACITY: Int = 50
    }
}
