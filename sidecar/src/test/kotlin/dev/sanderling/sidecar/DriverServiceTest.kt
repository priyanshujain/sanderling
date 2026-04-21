package dev.sanderling.sidecar

import dev.sanderling.driver.v1.DriverGrpc
import dev.sanderling.driver.v1.Duration
import dev.sanderling.driver.v1.Empty
import dev.sanderling.driver.v1.LaunchRequest
import dev.sanderling.driver.v1.Point
import dev.sanderling.driver.v1.PressKeyRequest
import dev.sanderling.driver.v1.RecentLogsRequest
import dev.sanderling.driver.v1.SwipeRequest
import dev.sanderling.driver.v1.Text
import io.grpc.ManagedChannel
import io.grpc.inprocess.InProcessChannelBuilder
import io.grpc.inprocess.InProcessServerBuilder
import io.grpc.testing.GrpcCleanupRule
import org.junit.Rule
import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

private data class Quintuple<A, B, C, D, E>(val a: A, val b: B, val c: C, val d: D, val e: E)

class DriverServiceTest {

    @get:Rule val grpcCleanup: GrpcCleanupRule = GrpcCleanupRule()

    private fun newClient(backend: DriverBackend): DriverGrpc.DriverBlockingStub {
        val serverName = InProcessServerBuilder.generateName()
        val service = DriverService(platform = "android", serial = null, backend = backend)
        grpcCleanup.register(
            InProcessServerBuilder.forName(serverName).directExecutor().addService(service).build().start()
        )
        val channel: ManagedChannel = grpcCleanup.register(
            InProcessChannelBuilder.forName(serverName).directExecutor().build()
        )
        return DriverGrpc.newBlockingStub(channel)
    }

    @Test fun launchAndTerminateRoundTripBundle() {
        val backend = StubDriverBackend("android")
        val client = newClient(backend)

        client.launch(
            LaunchRequest.newBuilder()
                .setBundleId("com.example")
                .setLauncherActivity(".MainActivity")
                .setClearState(true)
                .build(),
        )
        assertEquals("com.example", backend.lastBundleId)
        assertEquals(1, backend.launchCount)

        client.terminate(Empty.getDefaultInstance())
        assertEquals(null, backend.lastBundleId)
    }

    @Test fun tapForwardsCoordinates() {
        val backend = StubDriverBackend("android")
        val client = newClient(backend)

        client.tap(Point.newBuilder().setX(100).setY(250).build())
        assertEquals(100 to 250, backend.lastTap)
    }

    @Test fun inputTextForwardsValue() {
        val backend = StubDriverBackend("android")
        val client = newClient(backend)

        client.inputText(Text.newBuilder().setValue("hello world").build())
        assertEquals("hello world", backend.lastInputText)
    }

    @Test fun screenshotReturnsBackendBytes() {
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun screenshot(): Triple<ByteArray, Int, Int> = Triple(byteArrayOf(1, 2, 3), 1080, 2340)
        }
        val client = newClient(backend)

        val image = client.screenshot(Empty.getDefaultInstance())
        assertEquals(1080, image.width)
        assertEquals(2340, image.height)
        assertEquals(3, image.png.size())
    }

    @Test fun hierarchyReturnsBackendJson() {
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun hierarchy(): String = "{\"x\":1}"
        }
        val client = newClient(backend)

        val hierarchy = client.hierarchy(Empty.getDefaultInstance())
        assertEquals("{\"x\":1}", hierarchy.json)
    }

    @Test fun waitForIdleHonorsDuration() {
        var observed: Long = -1L
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun waitForIdle(durationMillis: Long) {
                observed = durationMillis
            }
        }
        val client = newClient(backend)

        client.waitForIdle(Duration.newBuilder().setMillis(123).build())
        assertEquals(123L, observed)
    }

    @Test fun swipeForwardsEndpointsAndDuration() {
        var observed: Quintuple<Int, Int, Int, Int, Long>? = null
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun swipe(fromX: Int, fromY: Int, toX: Int, toY: Int, durationMillis: Long) {
                observed = Quintuple(fromX, fromY, toX, toY, durationMillis)
            }
        }
        val client = newClient(backend)

        client.swipe(
            SwipeRequest.newBuilder()
                .setFrom(Point.newBuilder().setX(10).setY(20).build())
                .setTo(Point.newBuilder().setX(30).setY(40).build())
                .setDurationMillis(250)
                .build(),
        )
        assertEquals(Quintuple(10, 20, 30, 40, 250L), observed)
    }

    @Test fun pressKeyForwardsValue() {
        var observed: String? = null
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun pressKey(key: String) {
                observed = key
            }
        }
        val client = newClient(backend)

        client.pressKey(PressKeyRequest.newBuilder().setKey("back").build())
        assertEquals("back", observed)
    }

    @Test fun recentLogsReturnsBackendEntries() {
        val backend = object : DriverBackend by StubDriverBackend("android") {
            override fun recentLogs(sinceUnixMillis: Long, minLevel: String): List<LogLine> {
                return listOf(LogLine(1, "E", "AndroidRuntime", "boom"))
            }
        }
        val client = newClient(backend)

        val response = client.recentLogs(
            RecentLogsRequest.newBuilder().setSinceUnixMillis(0).setLevelAtLeast("E").build(),
        )
        assertEquals(1, response.entriesCount)
        assertEquals("AndroidRuntime", response.getEntries(0).tag)
        assertEquals("boom", response.getEntries(0).message)
    }

    @Test fun healthReportsPlatformAndVersion() {
        val backend = StubDriverBackend("android")
        val client = newClient(backend)

        val status = client.health(Empty.getDefaultInstance())
        assertTrue(status.ready)
        assertEquals("android", status.platform)
        assertEquals(DriverService.VERSION, status.version)
    }
}
