package dev.uatu.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class ResolveActivityTest {

    @Test fun extractsActivityFromBriefOutput() {
        val output = """
            priority=0 preferredOrder=0 match=0x108000 specificIndex=-1 isDefault=false
            dev.uatu.sample/.MainActivity
        """.trimIndent()

        val activity = StubDriverBackend.parseResolvedActivity("dev.uatu.sample", output)
        assertEquals(".MainActivity", activity)
    }

    @Test fun extractsFullyQualifiedActivity() {
        val output = "com.example.app/com.example.app.ui.LaunchActivity"

        val activity = StubDriverBackend.parseResolvedActivity("com.example.app", output)
        assertEquals("com.example.app.ui.LaunchActivity", activity)
    }

    @Test fun returnsNullWhenPackageNotFound() {
        val output = "No activity found"

        val activity = StubDriverBackend.parseResolvedActivity("dev.uatu.sample", output)
        assertNull(activity)
    }

    @Test fun doesNotMatchDifferentPackagePrefix() {
        val output = "other.pkg/.MainActivity"

        val activity = StubDriverBackend.parseResolvedActivity("dev.uatu.sample", output)
        assertNull(activity)
    }
}
