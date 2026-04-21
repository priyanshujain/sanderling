package dev.sanderling.sdk

import java.io.IOException
import java.io.PipedInputStream
import java.io.PipedOutputStream
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class SocketClientTest {

    /** In-memory transport that pairs each connect() call with a matching
     *  server endpoint a test can read/write on. */
    class FakeTransport : AgentTransport {
        data class Endpoints(val client: AgentConnection, val server: AgentConnection)

        private val pending = LinkedBlockingQueue<Endpoints>()
        val connectCount = AtomicInteger(0)
        @Volatile var failNextConnect: Throwable? = null

        override fun connect(): AgentConnection {
            connectCount.incrementAndGet()
            failNextConnect?.let { cause ->
                failNextConnect = null
                throw IOException("connect failed", cause)
            }
            val toClient = PipedOutputStream()
            val fromServer = PipedInputStream(toClient, 64 * 1024)
            val toServer = PipedOutputStream()
            val fromClient = PipedInputStream(toServer, 64 * 1024)
            val client = pipedConnection(fromServer, toServer)
            val server = pipedConnection(fromClient, toClient)
            pending.offer(Endpoints(client, server))
            return client
        }

        fun nextServerEndpoint(timeoutMillis: Long = 2_000L): AgentConnection {
            val endpoints = pending.poll(timeoutMillis, TimeUnit.MILLISECONDS)
                ?: error("no server endpoint emerged within $timeoutMillis ms")
            return endpoints.server
        }

        private fun pipedConnection(input: PipedInputStream, output: PipedOutputStream): AgentConnection {
            return object : AgentConnection {
                override val input = input
                override val output = output
                override fun close() {
                    try { input.close() } catch (_: IOException) {}
                    try { output.close() } catch (_: IOException) {}
                }
            }
        }
    }

    class RecordingHandler : SocketClient.Handler {
        val connected = CountDownLatch(1)
        val disconnected = LinkedBlockingQueue<Throwable?>()
        val messages = CopyOnWriteArrayList<Message>()
        @Volatile var sender: SocketClient.MessageSender? = null

        override fun onConnected(sender: SocketClient.MessageSender) {
            this.sender = sender
            connected.countDown()
        }

        override fun onMessage(message: Message) {
            messages += message
        }

        override fun onDisconnected(cause: Throwable?) {
            disconnected.offer(cause ?: SentinelDisconnect)
        }

        fun waitForMessages(expected: Int, timeoutMillis: Long = 2_000L) {
            val deadline = System.currentTimeMillis() + timeoutMillis
            while (messages.size < expected && System.currentTimeMillis() < deadline) Thread.sleep(5L)
            assertEquals("expected $expected messages, got ${messages.size}", expected, messages.size)
        }
    }

    private object SentinelDisconnect : Throwable()

    private lateinit var client: SocketClient

    @After fun tearDown() {
        if (::client.isInitialized) client.stop()
    }

    @Test fun connectsAndDeliversIncomingMessages() {
        val transport = FakeTransport()
        val handler = RecordingHandler()
        client = SocketClient(transport, handler).also { it.start() }

        val server = transport.nextServerEndpoint()
        Protocol.write(server.output, Message.pause(1))
        Protocol.write(server.output, Message.resume(1))
        handler.waitForMessages(2)

        assertEquals(MessageType.PAUSE, handler.messages[0].type)
        assertEquals(1L, handler.messages[0].id)
        assertEquals(MessageType.RESUME, handler.messages[1].type)
    }

    @Test fun sendGoesThroughOutputStream() {
        val transport = FakeTransport()
        val handler = RecordingHandler()
        client = SocketClient(transport, handler).also { it.start() }

        assertTrue("connected latch must fire", handler.connected.await(2, TimeUnit.SECONDS))
        val sender = handler.sender ?: error("sender not set after onConnected")

        sender.send(Message.hello("0.0.1", "android", "com.x"))

        val server = transport.nextServerEndpoint()
        val received = Protocol.read(server.input)
        assertEquals(MessageType.HELLO, received.type)
        assertEquals("com.x", received.appPackage)
    }

    @Test fun reconnectsWithBackoffAfterConnectFailure() {
        val transport = FakeTransport()
        transport.failNextConnect = RuntimeException("no socket yet")
        val handler = RecordingHandler()

        val observedSleeps = CopyOnWriteArrayList<Long>()
        val shortBackoff = Backoff(initialDelayMillis = 25L, maxDelayMillis = 25L, multiplier = 1.0)
        client = SocketClient(
            transport,
            handler,
            backoff = shortBackoff,
            sleeper = { millis -> observedSleeps += millis },
        ).also { it.start() }

        assertTrue("connected latch must fire after retry", handler.connected.await(2, TimeUnit.SECONDS))
        assertTrue("should have retried at least once", transport.connectCount.get() >= 2)
        assertTrue("sleeper should have been called with backoff delay, got $observedSleeps",
            observedSleeps.any { it > 0L })
    }

    @Test fun reconnectsAfterServerClosesConnection() {
        val transport = FakeTransport()
        val handler = RecordingHandler()
        val shortBackoff = Backoff(initialDelayMillis = 10L, maxDelayMillis = 10L, multiplier = 1.0)
        client = SocketClient(transport, handler, backoff = shortBackoff).also { it.start() }

        val firstServer = transport.nextServerEndpoint()
        assertTrue(handler.connected.await(2, TimeUnit.SECONDS))
        firstServer.close()

        // Handler.onDisconnected should fire.
        val cause = handler.disconnected.poll(2, TimeUnit.SECONDS)
        assertNotNull("expected disconnect notification", cause)

        // A second connect() should happen — fetch the new server endpoint to prove it.
        val secondServer = transport.nextServerEndpoint(timeoutMillis = 2_000L)
        assertSame("second endpoint must exist", secondServer, secondServer)
        assertTrue("connect count should be >= 2, got ${transport.connectCount.get()}",
            transport.connectCount.get() >= 2)
    }

    @Test fun stopClosesConnection() {
        val transport = FakeTransport()
        val handler = RecordingHandler()
        client = SocketClient(transport, handler).also { it.start() }

        transport.nextServerEndpoint()
        assertTrue(handler.connected.await(2, TimeUnit.SECONDS))
        client.stop()

        val cause = handler.disconnected.poll(2, TimeUnit.SECONDS)
        assertNotNull("stop() should trigger onDisconnected", cause)
    }

    @Test fun backoffGrowsExponentially() {
        val backoff = Backoff(initialDelayMillis = 100L, maxDelayMillis = 800L, multiplier = 2.0)
        assertEquals(100L, backoff.next(0L))
        assertEquals(200L, backoff.next(100L))
        assertEquals(400L, backoff.next(200L))
        assertEquals(800L, backoff.next(400L))
        assertEquals(800L, backoff.next(800L))
    }
}
