package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class StabilityPollTest {
    @Test fun returnsAfterUninterruptedStableStreak() {
        val start = System.currentTimeMillis()
        pollUntilStable(3000L) { "stable" }
        val elapsed = System.currentTimeMillis() - start
        assertTrue(
            elapsed >= MIN_STABLE_STREAK_MILLIS,
            "must observe a stable streak of at least ${MIN_STABLE_STREAK_MILLIS}ms, elapsed=${elapsed}ms",
        )
        assertTrue(elapsed < 3000L, "should not run to cap when stable, elapsed=${elapsed}ms")
    }

    @Test fun streakResetsOnAnyChange() {
        // A late transition that fires after the prior streak has already
        // begun must reset the clock: the post-transition stable window has
        // to start over and meet MIN_STABLE_STREAK_MILLIS from scratch.
        var calls = 0
        val start = System.currentTimeMillis()
        // First 4 samples are "calm", then 1 transient change, then "stable"
        // forever - the calm prefix is meaningless because of the transition.
        pollUntilStable(3000L) {
            calls++
            when {
                calls <= 4 -> "calm"
                calls == 5 -> "transient"
                else -> "stable"
            }
        }
        val elapsed = System.currentTimeMillis() - start
        assertTrue(
            elapsed >= MIN_STABLE_STREAK_MILLIS,
            "post-transition streak must reach ${MIN_STABLE_STREAK_MILLIS}ms, elapsed=${elapsed}ms",
        )
        assertTrue(calls >= 10, "expected enough samples to span calm + transient + stable streak, got $calls")
    }

    @Test fun transitionalNullsForceLoopToKeepWaiting() {
        // Simulates a NavHost cross-fade where stabilitySnapshot returns null
        // for several samples (multi-screen detected) before the destination
        // screen settles. Streak must not start while null returns persist.
        var calls = 0
        val transitionalCount = 5
        pollUntilStable(3000L) {
            calls++
            if (calls <= transitionalCount) null else "stable"
        }
        assertTrue(
            calls >= transitionalCount + 2,
            "must consume the null prefix before starting the streak, calls=$calls",
        )
    }

    @Test fun stopsAtDeadlineWhenNeverStable() {
        var calls = 0
        val budget = 240L
        val start = System.currentTimeMillis()
        pollUntilStable(budget) {
            calls++
            "frame-$calls"
        }
        val elapsed = System.currentTimeMillis() - start
        assertTrue(elapsed in budget..(budget + 1000L), "expected to hit cap, elapsed=$elapsed")
    }

    @Test fun zeroBudgetReturnsImmediately() {
        var calls = 0
        pollUntilStable(0L) {
            calls++
            "x"
        }
        assertEquals(0, calls)
    }

    @Test fun structuralHashIgnoresBoundsAndIdenticalForSemanticallyEqualTrees() {
        val a = """
        {"attributes":{"resource-id":"LoginScreen","bounds":"[0,0,1080,2340]"},
         "children":[
           {"attributes":{"resource-id":"LoginEmail","bounds":"[10,10,1070,100]","text":"a@b"},"children":[]}
         ]}
        """.trimIndent()
        val b = """
        {"attributes":{"resource-id":"LoginScreen","bounds":"[0,0,1080,2342]"},
         "children":[
           {"attributes":{"resource-id":"LoginEmail","bounds":"[10,11,1070,101]","text":"a@b"},"children":[]}
         ]}
        """.trimIndent()
        assertEquals(structuralHash(a), structuralHash(b), "bounds-only flicker must not change hash")
    }

    @Test fun structuralHashDiffersWhenContentChanges() {
        val a = """{"attributes":{"resource-id":"LoginEmail","text":"a@b"},"children":[]}"""
        val b = """{"attributes":{"resource-id":"LoginEmail","text":"c@d"},"children":[]}"""
        assertTrue(structuralHash(a) != structuralHash(b), "text change must alter hash")
    }

    @Test fun stabilitySnapshotReturnsNullDuringNavHostCrossFade() {
        val midTransition = """
        {"attributes":{"resource-id":"root"},
         "children":[
           {"attributes":{"resource-id":"AddAccountScreen"},"children":[]},
           {"attributes":{"resource-id":"HomeScreen"},"children":[
             {"attributes":{"resource-id":"AccountCard","text":"a"},"children":[]}
           ]}
         ]}
        """.trimIndent()
        assertEquals(null, stabilitySnapshot(midTransition))
    }

    @Test fun stabilitySnapshotReturnsHashOnSingleScreen() {
        val singleScreen = """
        {"attributes":{"resource-id":"HomeScreen"},
         "children":[
           {"attributes":{"resource-id":"AccountCard","text":"a"},"children":[]}
         ]}
        """.trimIndent()
        val hash = stabilitySnapshot(singleScreen)
        assertTrue(hash != null && hash.isNotBlank(), "single-screen tree must yield a hash, got $hash")
    }

    @Test fun stabilitySnapshotIgnoresNonRouteAttributeValues() {
        // Random text content ending in "Screen" must not trip the detector;
        // only route-level attribute keys (resource-id, testTag, identifier)
        // are considered for the route-tag count.
        val tree = """
        {"attributes":{"resource-id":"HomeScreen"},
         "children":[
           {"attributes":{"text":"Welcome to MyScreen"},"children":[]}
         ]}
        """.trimIndent()
        assertTrue(stabilitySnapshot(tree) != null, "non-route attribute must not be counted as a screen")
    }

    @Test fun countRouteScreensCountsTestTagAndIdentifier() {
        val tree = """
        {"attributes":{"resource-id":"root"},
         "children":[
           {"attributes":{"testTag":"HomeScreen"},"children":[]},
           {"attributes":{"accessibilityIdentifier":"LedgerScreen"},"children":[]}
         ]}
        """.trimIndent()
        assertEquals(2, countRouteScreens(tree))
    }
}
