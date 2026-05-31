package dev.sanderling.sidecar

import dev.sanderling.driver.v1.DriverGrpc
import dev.sanderling.driver.v1.Empty
import io.grpc.ManagedChannel
import io.grpc.inprocess.InProcessChannelBuilder
import io.grpc.inprocess.InProcessServerBuilder
import io.grpc.testing.GrpcCleanupRule
import org.junit.Rule
import org.junit.Test
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.locks.ReentrantLock
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SnapshotHandlerTest {

    @get:Rule val grpcCleanup: GrpcCleanupRule = GrpcCleanupRule()

    private fun newClient(backend: DriverBackend): DriverGrpc.DriverBlockingStub {
        val serverName = InProcessServerBuilder.generateName()
        val service = DriverService(platform = "android", backend = backend)
        grpcCleanup.register(
            InProcessServerBuilder.forName(serverName).directExecutor().addService(service).build().start(),
        )
        val channel: ManagedChannel = grpcCleanup.register(
            InProcessChannelBuilder.forName(serverName).directExecutor().build(),
        )
        return DriverGrpc.newBlockingStub(channel)
    }

    @Test fun snapshotReturnsBothHierarchyAndScreenshot() {
        // The default snapshot() impl calls hierarchy() then screenshot() on
        // the same backend instance; using interface delegation here would
        // forward those calls to the delegate, not these overrides. Override
        // snapshot() directly so the test exercises the wire path end-to-end.
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun snapshot(): SnapshotSample =
                SnapshotSample("{\"x\":1}", Triple(byteArrayOf(7, 8, 9), 1080, 2340))
        }
        val client = newClient(backend)

        val response = client.snapshot(Empty.getDefaultInstance())
        assertEquals("{\"x\":1}", response.hierarchy.json)
        assertEquals(1080, response.screenshot.width)
        assertEquals(2340, response.screenshot.height)
        assertEquals(3, response.screenshot.png.size())
    }

    @Test fun defaultSnapshotPairsHierarchyAndScreenshotSequentially() {
        // Covers the DriverBackend default impl: hierarchy() runs before
        // screenshot() so a transitional re-fetch keeps the screenshot
        // aligned with the final hierarchy snapshot the runner accepts.
        val callOrder = mutableListOf<String>()
        val backend = object : DriverBackend {
            override fun launch(bundleId: String, clearState: Boolean, env: Map<String, String>) {}
            override fun terminate(bundleId: String) {}
            override fun tap(x: Int, y: Int) {}
            override fun tapSelector(selector: String) {}
            override fun inputText(text: String) {}
            override fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long) {}
            override fun pressKey(key: String) {}
            override fun screenshot(): Triple<ByteArray, Int, Int> {
                callOrder.add("screenshot")
                return Triple(byteArrayOf(1), 0, 0)
            }
            override fun hierarchy(): String {
                callOrder.add("hierarchy")
                return "{}"
            }
            override fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine> = emptyList()
            override fun waitForIdle(durationMillis: Long) {}
            override fun healthy(): Boolean = true
            override fun metrics(bundleId: String): MetricsSample = MetricsSample(0.0, 0L, 0L)
        }
        backend.snapshot()
        assertEquals(listOf("hierarchy", "screenshot"), callOrder)
    }

    @Test fun concurrentSnapshotsAreSerialized() {
        // The recording backend asserts no overlap: a sleep-bearing snapshot()
        // makes any race observable as inFlight > 1.
        val inFlight = AtomicInteger(0)
        val maxObserved = AtomicInteger(0)
        val callCount = AtomicInteger(0)
        val lock = ReentrantLock()
        val recordingBackend = object : DriverBackend by StubDriverBackend("android") {
            override fun snapshot(): SnapshotSample {
                val now = inFlight.incrementAndGet()
                try {
                    lock.lock()
                    try {
                        if (now > maxObserved.get()) maxObserved.set(now)
                    } finally {
                        lock.unlock()
                    }
                    Thread.sleep(50)
                    callCount.incrementAndGet()
                    return SnapshotSample("{}", Triple(ByteArray(0), 0, 0))
                } finally {
                    inFlight.decrementAndGet()
                }
            }
        }
        // Use a real (multi-threaded) executor on the server side so the service
        // is not artificially serialized by directExecutor.
        val serverName = InProcessServerBuilder.generateName()
        val service = DriverService(platform = "android", backend = recordingBackend)
        grpcCleanup.register(
            InProcessServerBuilder.forName(serverName).addService(service).build().start(),
        )
        val channel: ManagedChannel = grpcCleanup.register(
            InProcessChannelBuilder.forName(serverName).build(),
        )
        val stub = DriverGrpc.newBlockingStub(channel)

        val pool = Executors.newFixedThreadPool(4)
        try {
            val tasks = (1..4).map {
                pool.submit { stub.snapshot(Empty.getDefaultInstance()) }
            }
            for (task in tasks) task.get(5, TimeUnit.SECONDS)
        } finally {
            pool.shutdownNow()
        }
        assertEquals(4, callCount.get())
        assertTrue(
            maxObserved.get() <= 1,
            "snapshot calls overlapped (maxObserved=${maxObserved.get()}); the service lock did not serialize them",
        )
    }
}
