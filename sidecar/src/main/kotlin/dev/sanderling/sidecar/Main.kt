package dev.sanderling.sidecar

import io.grpc.Server
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder
import java.net.InetSocketAddress
import java.util.concurrent.CountDownLatch

class SidecarServer(
    private val port: Int,
    private val service: DriverService,
) {
    private var grpcServer: Server? = null
    private val shutdownLatch = CountDownLatch(1)

    fun start(): Int {
        val server = NettyServerBuilder.forAddress(InetSocketAddress("127.0.0.1", port))
            .addService(service)
            .build()
        server.start()
        grpcServer = server
        Runtime.getRuntime().addShutdownHook(Thread {
            stop()
        })
        return server.port
    }

    fun awaitTermination() {
        shutdownLatch.await()
    }

    fun stop() {
        grpcServer?.shutdown()
        shutdownLatch.countDown()
    }
}

fun main(arguments: Array<String>) {
    val port = arguments.indexOf("--port").let { index ->
        if (index >= 0 && index + 1 < arguments.size) arguments[index + 1].toInt() else 0
    }
    val platform = arguments.indexOf("--platform").let { index ->
        if (index >= 0 && index + 1 < arguments.size) arguments[index + 1] else "android"
    }
    val serial = arguments.indexOf("--serial").let { index ->
        if (index >= 0 && index + 1 < arguments.size) arguments[index + 1] else null
    }
    val udid = arguments.indexOf("--udid").let { index ->
        if (index >= 0 && index + 1 < arguments.size) arguments[index + 1] else null
    }

    val backend: DriverBackend = when (platform) {
        "android" -> MaestroDriverBackend(serial)
        "ios" -> IosDriverBackend(udid ?: error("--udid required for ios platform"))
        else -> StubDriverBackend(platform)
    }
    val service = DriverService(platform = platform, backend = backend)
    val server = SidecarServer(port, service)
    val boundPort = server.start()
    println("sanderling-sidecar listening on 127.0.0.1:$boundPort platform=$platform")
    System.out.flush()
    server.awaitTermination()
}
