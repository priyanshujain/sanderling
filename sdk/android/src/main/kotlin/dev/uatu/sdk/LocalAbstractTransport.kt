package dev.uatu.sdk

import android.net.LocalSocket
import android.net.LocalSocketAddress
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream

class LocalAbstractTransport(private val socketName: String) : AgentTransport {
    @Throws(IOException::class)
    override fun connect(): AgentConnection {
        val socket = LocalSocket()
        socket.connect(LocalSocketAddress(socketName, LocalSocketAddress.Namespace.ABSTRACT))
        return LocalSocketConnection(socket)
    }

    private class LocalSocketConnection(private val socket: LocalSocket) : AgentConnection {
        override val input: InputStream = socket.inputStream
        override val output: OutputStream = socket.outputStream
        override fun close() {
            try { socket.shutdownInput() } catch (_: IOException) {}
            try { socket.shutdownOutput() } catch (_: IOException) {}
            socket.close()
        }
    }
}
