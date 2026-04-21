package dev.sanderling.sdk

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicReference

fun interface FrameCallbackPoster {
    fun postFrameCallback(callback: () -> Unit)
}

class Pauser(
    private val poster: FrameCallbackPoster,
    private val pauseTimeoutMillis: Long = 5_000L,
) {
    @Volatile private var currentGate: Semaphore? = null

    /**
     * Schedules extractors to run on the frame-callback thread (the SDK's
     * "main thread" analogue) and blocks that thread after they complete
     * until release() is called or pauseTimeoutMillis elapses.
     * Returns the extractor output. Must be called from a worker thread.
     */
    @Throws(TimeoutException::class)
    fun pauseAndSnapshot(extractors: () -> Map<String, Any?>): Map<String, Any?> {
        val gate = Semaphore(0)
        val ready = CountDownLatch(1)
        val captured = AtomicReference<Result<Map<String, Any?>>>()

        poster.postFrameCallback {
            captured.set(runCatching { extractors() })
            ready.countDown()
            try {
                gate.tryAcquire(pauseTimeoutMillis, TimeUnit.MILLISECONDS)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            }
        }
        currentGate = gate

        if (!ready.await(pauseTimeoutMillis, TimeUnit.MILLISECONDS)) {
            throw TimeoutException("extractors did not run within ${pauseTimeoutMillis}ms")
        }
        return captured.get().getOrThrow()
    }

    fun release() {
        currentGate?.release()
    }
}
