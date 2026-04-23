package app.folio.sanderling

import kotlinx.cinterop.*
import platform.posix.*

@OptIn(ExperimentalForeignApi::class)
internal class TcpConnection private constructor(private val fd: Int) {
    companion object {
        fun connect(host: String, port: Int): TcpConnection {
            val sock = socket(AF_INET, SOCK_STREAM, 0)
            check(sock >= 0) { "socket() failed" }
            memScoped {
                val addr = alloc<sockaddr_in>()
                addr.sin_family = AF_INET.convert()
                addr.sin_port = htons(port.convert())
                inet_pton(AF_INET, host, addr.sin_addr.ptr)
                val result = platform.posix.connect(sock, addr.ptr.reinterpret(), sizeOf<sockaddr_in>().convert())
                if (result < 0) {
                    close(sock)
                    error("connect() failed: errno=$errno")
                }
            }
            return TcpConnection(sock)
        }
    }

    fun writeFrame(data: ByteArray) {
        val len = data.size
        writeAll(byteArrayOf((len ushr 24).toByte(), (len ushr 16).toByte(), (len ushr 8).toByte(), len.toByte()))
        writeAll(data)
    }

    fun readFrame(): ByteArray {
        val header = readAll(4)
        val len = ((header[0].toInt() and 0xFF) shl 24) or
                  ((header[1].toInt() and 0xFF) shl 16) or
                  ((header[2].toInt() and 0xFF) shl 8) or
                  (header[3].toInt() and 0xFF)
        check(len in 0..16_777_216) { "bad frame length: $len" }
        return readAll(len)
    }

    private fun writeAll(data: ByteArray) {
        data.usePinned { pinned ->
            var offset = 0
            while (offset < data.size) {
                val n = send(fd, pinned.addressOf(offset), (data.size - offset).convert(), 0).toInt()
                check(n > 0) { "send() failed: errno=$errno" }
                offset += n
            }
        }
    }

    private fun readAll(count: Int): ByteArray {
        val buf = ByteArray(count)
        buf.usePinned { pinned ->
            var offset = 0
            while (offset < count) {
                val n = recv(fd, pinned.addressOf(offset), (count - offset).convert(), 0).toInt()
                check(n > 0) { "recv() returned $n" }
                offset += n
            }
        }
        return buf
    }

    fun close() {
        platform.posix.close(fd)
    }
}
