package dev.uatu.sdk

import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class UatuRuntimeTest {

    private lateinit var transport: SocketClientTest.FakeTransport
    private lateinit var frameThread: PauserTest.FakeFrameThread
    private lateinit var runtime: UatuRuntime

    private fun newRuntime(): UatuRuntime {
        transport = SocketClientTest.FakeTransport()
        frameThread = PauserTest.FakeFrameThread()
        val pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L)
        return UatuRuntime(
            transport = transport,
            pauser = pauser,
            version = "0.0.1",
            platform = "android",
            appPackage = "com.example.uatu_test",
        ).also { runtime = it }
    }

    @After fun tearDown() {
        if (::runtime.isInitialized) runtime.stop()
        if (::frameThread.isInitialized) frameThread.shutdown()
    }

    @Test fun startSendsHelloWithSdkMetadata() {
        newRuntime().start()
        val server = transport.nextServerEndpoint()
        val hello = Protocol.read(server.input)
        assertEquals(MessageType.HELLO, hello.type)
        assertEquals("0.0.1", hello.version)
        assertEquals("android", hello.platform)
        assertEquals("com.example.uatu_test", hello.appPackage)
    }

    @Test fun pauseTriggersExtractorsAndReturnsState() {
        newRuntime().start()
        val server = transport.nextServerEndpoint()
        Protocol.read(server.input) // drain HELLO

        runtime.register("screen") { "customer_ledger" }
        runtime.register("ledger.balance") { 1500 }

        Protocol.write(server.output, Message.pause(7))
        val state = Protocol.read(server.input)
        assertEquals(MessageType.STATE, state.type)
        assertEquals(7L, state.id)
        val snapshots = state.snapshots ?: error("state.snapshots must not be null")
        assertEquals("customer_ledger", snapshots["screen"])
        assertEquals(1500, snapshots["ledger.balance"])

        Protocol.write(server.output, Message.resume(7))
        // After resume, the frame thread can accept subsequent callbacks.
        Protocol.write(server.output, Message.pause(8))
        val nextState = Protocol.read(server.input)
        assertEquals(MessageType.STATE, nextState.type)
        assertEquals(8L, nextState.id)
    }

    @Test fun extractorInvocationOrderMatchesRegistration() {
        val runtime = newRuntime()
        val order = CopyOnWriteArrayList<String>()
        runtime.register("first") { order += "first"; 1 }
        runtime.register("second") { order += "second"; 2 }
        runtime.register("third") { order += "third"; 3 }

        val snapshot = runtime.snapshot()
        assertEquals(listOf("first", "second", "third"), order)
        assertEquals(listOf("first", "second", "third"), snapshot.keys.toList())
        assertArrayEquals(arrayOf(1, 2, 3), snapshot.values.toList().toTypedArray())
    }

    @Test fun extractorThrowIsIsolatedAndReportsNull() {
        val runtime = newRuntime()
        runtime.register("ok") { "value" }
        runtime.register("boom") { throw IllegalStateException("oops") }
        runtime.register("later") { 42 }

        val snapshot = runtime.snapshot()
        assertEquals("value", snapshot["ok"])
        assertEquals(null, snapshot["boom"])
        assertEquals(42, snapshot["later"])
    }

    @Test fun concurrentExtractorRegistrationIsSafe() {
        val runtime = newRuntime()
        val registrations = 500
        val pool = Executors.newFixedThreadPool(8)
        val latch = CountDownLatch(registrations)
        val index = AtomicInteger(0)
        repeat(registrations) {
            pool.submit {
                val id = index.getAndIncrement()
                runtime.register("ext-$id") { id }
                latch.countDown()
            }
        }
        assertTrue(latch.await(5, TimeUnit.SECONDS))
        pool.shutdown()

        val snapshot = runtime.snapshot()
        assertEquals(registrations, snapshot.size)
    }

    @Test fun helloIsSentOnReconnect() {
        val runtime = newRuntime()
        val shortBackoff = Backoff(initialDelayMillis = 10L, maxDelayMillis = 10L, multiplier = 1.0)
        // Swap in a client with short backoff by re-creating the runtime's client indirectly.
        // We'll re-use the existing runtime instead and just close the first connection.
        runtime.start()
        val firstServer = transport.nextServerEndpoint()
        Protocol.read(firstServer.input) // drain first HELLO
        firstServer.close()

        val secondServer = transport.nextServerEndpoint(timeoutMillis = 3_000L)
        val secondHello = Protocol.read(secondServer.input)
        assertEquals(MessageType.HELLO, secondHello.type)
    }

    @Test fun registerBeforeStartQueuesCorrectlyOnceStarted() {
        val runtime = newRuntime()
        runtime.register("early") { 1 }
        runtime.register("middle") { 2 }
        runtime.start()
        runtime.register("late") { 3 }

        val snapshot = runtime.snapshot()
        assertEquals(3, snapshot.size)
        assertEquals(1, snapshot["early"])
        assertEquals(2, snapshot["middle"])
        assertEquals(3, snapshot["late"])
    }
}
