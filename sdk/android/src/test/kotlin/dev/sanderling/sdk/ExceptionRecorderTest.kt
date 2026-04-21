package dev.sanderling.sdk

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ExceptionRecorderTest {

    @Test fun recordsClassMessageAndStackTrace() {
        val recorder = ExceptionRecorder()
        recorder.record(RuntimeException("boom"))

        val drained = recorder.drain()
        assertEquals(1, drained.size)
        val entry = drained[0]
        assertEquals("java.lang.RuntimeException", entry.className)
        assertEquals("boom", entry.message)
        assertTrue(
            "stackTrace should include the class name, got: ${entry.stackTrace}",
            entry.stackTrace.contains("RuntimeException"),
        )
    }

    @Test fun drainClearsBuffer() {
        val recorder = ExceptionRecorder()
        recorder.record(RuntimeException("first"))
        recorder.record(RuntimeException("second"))
        assertEquals(2, recorder.drain().size)
        assertEquals(0, recorder.drain().size)
    }

    @Test fun dropsOldestWhenOverCapacity() {
        val recorder = ExceptionRecorder(capacity = 2)
        recorder.record(RuntimeException("a"))
        recorder.record(RuntimeException("b"))
        recorder.record(RuntimeException("c"))

        val drained = recorder.drain()
        assertEquals(2, drained.size)
        assertEquals("b", drained[0].message)
        assertEquals("c", drained[1].message)
    }

    @Test fun installChainsExistingHandler() {
        val recorder = ExceptionRecorder()
        val original = Thread.getDefaultUncaughtExceptionHandler()
        var chainedInvoked = false
        Thread.setDefaultUncaughtExceptionHandler { _, _ -> chainedInvoked = true }
        try {
            recorder.install()
            // Simulate an uncaught exception by invoking the installed handler
            // directly — we don't need to actually terminate a thread.
            Thread.getDefaultUncaughtExceptionHandler()!!.uncaughtException(
                Thread.currentThread(),
                IllegalStateException("chain me"),
            )
            assertTrue("chained handler should have fired", chainedInvoked)
            assertEquals(1, recorder.drain().size)
        } finally {
            recorder.uninstall()
            Thread.setDefaultUncaughtExceptionHandler(original)
        }
    }
}
