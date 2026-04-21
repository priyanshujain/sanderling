package dev.sanderling.sdk

import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.atomic.AtomicBoolean

interface AgentTransport {
    @Throws(IOException::class)
    fun connect(): AgentConnection
}

interface AgentConnection {
    val input: InputStream
    val output: OutputStream
    fun close()
}

data class Backoff(
    val initialDelayMillis: Long = 500L,
    val maxDelayMillis: Long = 10_000L,
    val multiplier: Double = 2.0,
) {
    fun next(previousDelayMillis: Long): Long =
        if (previousDelayMillis <= 0L) initialDelayMillis
        else minOf((previousDelayMillis * multiplier).toLong(), maxDelayMillis)
}

class SocketClient(
    private val transport: AgentTransport,
    private val handler: Handler,
    private val backoff: Backoff = Backoff(),
    private val threadFactory: (Runnable) -> Thread = { runnable -> Thread(runnable, "sanderling-agent-reader") },
    private val sleeper: (Long) -> Unit = { millis -> if (millis > 0L) Thread.sleep(millis) },
) {
    interface Handler {
        fun onConnected(sender: MessageSender)
        fun onMessage(message: Message)
        fun onDisconnected(cause: Throwable?)
    }

    fun interface MessageSender {
        @Throws(IOException::class)
        fun send(message: Message)
    }

    private val running = AtomicBoolean(false)
    @Volatile private var workerThread: Thread? = null
    @Volatile private var connection: AgentConnection? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        val thread = threadFactory { runLoop() }
        thread.isDaemon = true
        workerThread = thread
        thread.start()
    }

    fun stop() {
        if (!running.compareAndSet(true, false)) return
        try { connection?.close() } catch (_: IOException) {}
        workerThread?.interrupt()
        try { workerThread?.join(1_000L) } catch (_: InterruptedException) { Thread.currentThread().interrupt() }
    }

    private fun runLoop() {
        var delayMillis = 0L
        while (running.get()) {
            val connection = try {
                transport.connect()
            } catch (e: IOException) {
                handler.onDisconnected(e)
                if (!running.get()) return
                delayMillis = backoff.next(delayMillis)
                try { sleeper(delayMillis) } catch (_: InterruptedException) { return }
                continue
            }
            this.connection = connection
            delayMillis = 0L
            serve(connection)
            if (!running.get()) return
            delayMillis = backoff.next(delayMillis)
            try { sleeper(delayMillis) } catch (_: InterruptedException) { return }
        }
    }

    private fun serve(connection: AgentConnection) {
        val sender = MessageSender { message ->
            synchronized(connection.output) {
                Protocol.write(connection.output, message)
            }
        }
        handler.onConnected(sender)
        var disconnectCause: Throwable? = null
        try {
            while (running.get()) {
                val message = Protocol.read(connection.input)
                handler.onMessage(message)
            }
        } catch (e: IOException) {
            disconnectCause = e
        } finally {
            try { connection.close() } catch (_: IOException) {}
            this.connection = null
            handler.onDisconnected(disconnectCause)
        }
    }
}
