package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class IdleDetectionTest {
    @Test fun idleWhenNoAnimatingFlag() {
        val output = """
            Window #0 Window{abc123 StatusBar}:
              mDisplayId=0 mSession=Session{def456}
              mAnimating=false
        """.trimIndent()
        assertTrue(StubDriverBackend.isWindowDumpIdle(output))
    }

    @Test fun busyWhenAnimatingTrue() {
        val output = """
            Window #0 Window{abc123 Launcher}:
              mDisplayId=0 mSession=Session{def456}
              mAnimating=true
        """.trimIndent()
        assertFalse(StubDriverBackend.isWindowDumpIdle(output))
    }

    @Test fun idleWhenOutputEmpty() {
        assertTrue(StubDriverBackend.isWindowDumpIdle(""))
    }

    @Test fun busyWhenMultipleWindowsOneAnimating() {
        val output = """
            Window #0 Window{abc StatusBar}:
              mAnimating=false
            Window #1 Window{def Launcher}:
              mAnimating=true
        """.trimIndent()
        assertFalse(StubDriverBackend.isWindowDumpIdle(output))
    }
}
