package dev.sanderling.sdk

import android.util.Log

internal class UatuRuntime(
    transport: AgentTransport,
    private val pauser: Pauser,
    private val version: String,
    private val platform: String,
    private val appPackage: String,
    private val exceptionRecorder: ExceptionRecorder = ExceptionRecorder(),
) {
    private val extractors = LinkedHashMap<String, () -> Any?>()
    @Volatile private var sender: SocketClient.MessageSender? = null
    private val socketClient = SocketClient(transport, AgentHandler())

    fun start() {
        exceptionRecorder.install()
        socketClient.start()
    }

    fun stop() {
        socketClient.stop()
        exceptionRecorder.uninstall()
    }

    fun register(name: String, extractor: () -> Any?) {
        synchronized(extractors) { extractors[name] = extractor }
    }

    fun reportError(throwable: Throwable) {
        exceptionRecorder.record(throwable)
    }

    internal fun snapshot(): Map<String, Any?> {
        val drained = synchronized(extractors) { LinkedHashMap(extractors) }
        val result = LinkedHashMap<String, Any?>(drained.size)
        for ((name, extractor) in drained) {
            result[name] = runCatching { extractor() }
                .onFailure { cause -> Log.w(LOG_TAG, "extractor $name threw: $cause") }
                .getOrNull()
        }
        return result
    }

    private inner class AgentHandler : SocketClient.Handler {
        override fun onConnected(sender: SocketClient.MessageSender) {
            this@UatuRuntime.sender = sender
            try {
                sender.send(Message.hello(version, platform, appPackage))
            } catch (cause: Exception) {
                Log.w(LOG_TAG, "failed to send HELLO: $cause")
            }
        }

        override fun onMessage(message: Message) {
            when (message.type) {
                MessageType.PAUSE -> handlePause(message.id)
                MessageType.RESUME -> pauser.release()
                MessageType.GOODBYE -> socketClient.stop()
                else -> Log.w(LOG_TAG, "unexpected message type ${message.type} from host")
            }
        }

        override fun onDisconnected(cause: Throwable?) {
            sender = null
            pauser.release()
        }

        private fun handlePause(id: Long) {
            val snapshots = try {
                pauser.pauseAndSnapshot { snapshot() }
            } catch (cause: Exception) {
                Log.w(LOG_TAG, "snapshot failed: $cause")
                emptyMap()
            }
            val exceptions = exceptionRecorder.drain().map { entry ->
                mapOf(
                    "class" to entry.className,
                    "message" to entry.message,
                    "stack_trace" to entry.stackTrace,
                    "unix_millis" to entry.unixMillis,
                )
            }.takeIf { it.isNotEmpty() }
            val activeSender = sender ?: return
            try {
                activeSender.send(Message.state(id, snapshots, exceptions))
            } catch (cause: Exception) {
                Log.w(LOG_TAG, "failed to send STATE: $cause")
            }
        }
    }

    companion object {
        private const val LOG_TAG = "Uatu"
    }
}
