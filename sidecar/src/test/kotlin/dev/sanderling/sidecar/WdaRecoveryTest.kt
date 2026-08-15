package dev.sanderling.sidecar

import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import kotlin.concurrent.thread
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class WdaRecoveryTest {

    private fun recovery(isAlive: () -> Boolean, restart: () -> Unit) =
        WdaRecovery(isAlive = isAlive, restart = restart, log = {})

    @Test fun aliveChannelSkipsRestartAndRetriesReads() {
        val restarts = AtomicInteger(0)
        val recovery = recovery(
            isAlive = { true },
            restart = { restarts.incrementAndGet() },
        )
        var calls = 0

        val result = recovery.run(replay = true) {
            calls++
            if (calls == 1) throw IOException("connection reset")
            "ok"
        }

        assertEquals("ok", result)
        assertEquals(2, calls)
        assertEquals(0, restarts.get())
    }

    @Test fun aliveChannelSurfacesUnavailableForActions() {
        val restarts = AtomicInteger(0)
        val recovery = recovery(
            isAlive = { true },
            restart = { restarts.incrementAndGet() },
        )

        val thrown = assertFailsWith<io.grpc.StatusRuntimeException> {
            recovery.run(replay = false) {
                throw IOException("connection reset")
            }
        }

        assertEquals(io.grpc.Status.Code.UNAVAILABLE, thrown.status.code)
        assertEquals(0, restarts.get())
    }

    @Test fun deadChannelRestartsOnceThenRetries() {
        val alive = AtomicBoolean(false)
        val restarts = AtomicInteger(0)
        val recovery = recovery(
            isAlive = { alive.get() },
            restart = {
                restarts.incrementAndGet()
                alive.set(true)
            },
        )
        var calls = 0

        val result = recovery.run(replay = true) {
            calls++
            if (calls == 1) throw IOException("connection refused")
            "ok"
        }

        assertEquals("ok", result)
        assertEquals(1, restarts.get())
    }

    @Test fun concurrentFailuresRestartOnly() {
        val alive = AtomicBoolean(false)
        val restarts = AtomicInteger(0)
        val recovery = recovery(
            isAlive = { alive.get() },
            restart = {
                Thread.sleep(100)
                restarts.incrementAndGet()
                alive.set(true)
            },
        )
        val started = CountDownLatch(2)
        val threads = (1..2).map {
            thread {
                started.countDown()
                started.await()
                recovery.run(replay = true) {
                    if (!alive.get()) throw IOException("connection refused")
                    "ok"
                }
            }
        }
        threads.forEach { it.join() }

        assertEquals(1, restarts.get())
    }

    @Test fun restartFailureSurfacesWdaReconnectFailed() {
        val recovery = recovery(
            isAlive = { false },
            restart = { throw IllegalStateException("xcodebuild died") },
        )

        val thrown = assertFailsWith<IllegalStateException> {
            recovery.run(replay = true) {
                throw IOException("connection refused")
            }
        }

        assertTrue(thrown.message.orEmpty().contains("WDA reconnect failed"))
    }

    @Test fun nonIoFailurePropagatesWithoutRecovery() {
        val restarts = AtomicInteger(0)
        val probes = AtomicInteger(0)
        val recovery = recovery(
            isAlive = { probes.incrementAndGet() > 0 },
            restart = { restarts.incrementAndGet() },
        )

        assertFailsWith<IllegalArgumentException> {
            recovery.run(replay = true) {
                throw IllegalArgumentException("bad selector")
            }
        }

        assertEquals(0, restarts.get())
        assertEquals(0, probes.get())
    }

    @Test fun readRetryFailureSurfacesUnavailable() {
        val recovery = recovery(isAlive = { true }, restart = {})

        val thrown = assertFailsWith<io.grpc.StatusRuntimeException> {
            recovery.run(replay = true) {
                throw IOException("connection reset")
            }
        }

        assertEquals(io.grpc.Status.Code.UNAVAILABLE, thrown.status.code)
    }
}
