package dev.uatu.sdk

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class PauserTest {

    // Runs posted callbacks on a dedicated "main thread" executor, modelling
    // Choreographer's contract that callbacks fire off-thread from the caller.
    class FakeFrameThread : FrameCallbackPoster {
        val executor = Executors.newSingleThreadExecutor { runnable -> Thread(runnable, "fake-frame-thread") }
        val threadRef = AtomicReference<Thread>()

        override fun postFrameCallback(callback: () -> Unit) {
            executor.submit {
                threadRef.compareAndSet(null, Thread.currentThread())
                callback()
            }
        }

        fun shutdown() { executor.shutdownNow() }
    }

    private lateinit var frameThread: FakeFrameThread

    @After fun tearDown() {
        if (::frameThread.isInitialized) frameThread.shutdown()
    }

    @Test fun extractorsRunOnFrameThreadAndSnapshotReturns() {
        frameThread = FakeFrameThread()
        val pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L)
        val extractorThread = AtomicReference<Thread>()

        // Run on a worker thread so we can observe the separation.
        val snapshot = runOnWorker {
            pauser.pauseAndSnapshot {
                extractorThread.set(Thread.currentThread())
                mapOf("screen" to "home", "count" to 3)
            }.also { snapshot ->
                // Immediately release so the frame thread can exit the callback.
                pauser.release()
                snapshot
            }
        }

        assertEquals("home", snapshot["screen"])
        assertEquals(3, snapshot["count"])
        assertNotNull("extractor must have run", extractorThread.get())
        assertEquals("fake-frame-thread", extractorThread.get().name)
    }

    @Test fun frameThreadStaysBlockedUntilRelease() {
        frameThread = FakeFrameThread()
        val pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L)
        val releasedMarker = AtomicBoolean(false)

        val latch = CountDownLatch(1)
        val worker = Thread {
            pauser.pauseAndSnapshot { emptyMap() }
            // Now the frame thread is blocked inside the callback. Verify by
            // posting another callback and checking it does NOT run until we release.
            val secondCallbackRan = CountDownLatch(1)
            frameThread.postFrameCallback { secondCallbackRan.countDown() }
            assertFalse("second callback should be queued, not run",
                secondCallbackRan.await(200, TimeUnit.MILLISECONDS))

            pauser.release()
            assertTrue("second callback should run after release",
                secondCallbackRan.await(2, TimeUnit.SECONDS))
            releasedMarker.set(true)
            latch.countDown()
        }
        worker.start()

        assertTrue("worker must finish", latch.await(5, TimeUnit.SECONDS))
        assertTrue("release must have happened", releasedMarker.get())
    }

    @Test fun timeoutPropagatesWhenFrameThreadNeverRuns() {
        val stuck = FrameCallbackPoster { /* never invokes callback */ }
        val pauser = Pauser(stuck, pauseTimeoutMillis = 150L)

        try {
            pauser.pauseAndSnapshot { emptyMap() }
            fail("expected TimeoutException")
        } catch (_: TimeoutException) {
            // pass
        }
    }

    @Test fun extractorExceptionBubbles() {
        frameThread = FakeFrameThread()
        val pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L)

        try {
            runOnWorker {
                pauser.pauseAndSnapshot {
                    pauser.release() // drop the lock before throwing so main can unwind
                    throw IllegalStateException("extractor boom")
                }
            }
            fail("expected IllegalStateException")
        } catch (e: IllegalStateException) {
            assertEquals("extractor boom", e.message)
        }
    }

    @Test fun releaseWithoutActivePauseIsNoOp() {
        frameThread = FakeFrameThread()
        val pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L)
        pauser.release() // should not throw
    }

    private fun <T> runOnWorker(block: () -> T): T {
        val result = AtomicReference<Result<T>>()
        val thread = Thread { result.set(runCatching { block() }) }
        thread.start()
        thread.join(5_000L)
        return result.get().getOrThrow()
    }
}
