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

    @Test fun slowSnapshotReadsCountTowardTheStreak() {
        // The streak runs from the START of the read that opened the run of
        // identical snapshots, so the read's own duration is charged to it.
        // Every other test here uses an instant lambda and so passes under
        // either semantics; this one is the difference. StubDriverBackend's
        // waitForIdle polls a real `uiautomator dump`, which costs hundreds of
        // milliseconds, so the slow read is the case it runs in.
        val readMillis = 400L
        val sampleStarts = mutableListOf<Long>()
        val sampleEnds = mutableListOf<Long>()
        val start = System.currentTimeMillis()
        pollUntilStable(5000L) {
            sampleStarts += System.currentTimeMillis() - start
            Thread.sleep(readMillis)
            sampleEnds += System.currentTimeMillis() - start
            "stable"
        }
        val elapsed = System.currentTimeMillis() - start

        // One read plus one interval plus one read already clears 750ms, so the
        // poll settles for two samples where an instant read takes four.
        assertEquals(
            2,
            sampleStarts.size,
            "a ${readMillis}ms read should clear the streak in two samples, starts=$sampleStarts",
        )
        assertTrue(elapsed >= MIN_STABLE_STREAK_MILLIS, "elapsed=${elapsed}ms")

        // What the poll actually watched: the last read began this long after
        // the first one returned. It is the poll interval, not the streak, and
        // a caller that needs MIN_STABLE_STREAK_MILLIS of observed quiet has to
        // ask for a wider streak rather than assume this one delivers it.
        val observedQuiet = sampleStarts.last() - sampleEnds.first()
        assertTrue(
            observedQuiet < MIN_STABLE_STREAK_MILLIS,
            "the poll returned having observed ${observedQuiet}ms of quiet, not " +
                "${MIN_STABLE_STREAK_MILLIS}ms; if that changed, the streak semantics changed",
        )
    }

    @Test fun streakResetsOnAnyChange() {
        // A late transition that fires after the prior streak has already
        // begun must reset the clock: the post-transition stable window has
        // to start over and meet MIN_STABLE_STREAK_MILLIS from scratch.
        var calls = 0
        var transientAt = 0L
        // A short "calm" prefix, one transient change, then "stable" forever.
        // The prefix is meaningless because of the transition: only the streak
        // that starts after it can end the wait.
        pollUntilStable(3000L) {
            calls++
            when {
                calls <= 2 -> "calm"
                calls == 3 -> {
                    transientAt = System.currentTimeMillis()
                    "transient"
                }
                else -> "stable"
            }
        }
        val sinceTransition = System.currentTimeMillis() - transientAt
        assertTrue(calls > 3, "must keep sampling past the transition, got $calls")
        assertTrue(
            sinceTransition >= MIN_STABLE_STREAK_MILLIS,
            "the calm prefix must not count: a full ${MIN_STABLE_STREAK_MILLIS}ms streak has to " +
                "start over after the transition, returned ${sinceTransition}ms after it",
        )
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
