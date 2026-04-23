package app.folio.sanderling

import kotlinx.cinterop.ExperimentalForeignApi
import platform.darwin.*

@OptIn(ExperimentalForeignApi::class)
internal object IosPauser {
    private val snapshotReady = dispatch_semaphore_create(0)
    private val resumeGate = dispatch_semaphore_create(0)
    private var capturedSnapshot: Map<String, Any?> = emptyMap()

    fun pauseAndSnapshot(extractors: () -> Map<String, Any?>): Map<String, Any?> {
        dispatch_async(dispatch_get_main_queue()) {
            capturedSnapshot = runCatching { extractors() }.getOrElse { emptyMap() }
            dispatch_semaphore_signal(snapshotReady)
            dispatch_semaphore_wait(resumeGate, dispatch_time(DISPATCH_TIME_NOW, 5_000_000_000L))
        }
        dispatch_semaphore_wait(snapshotReady, dispatch_time(DISPATCH_TIME_NOW, 5_000_000_000L))
        return capturedSnapshot
    }

    fun release() {
        dispatch_semaphore_signal(resumeGate)
    }
}
