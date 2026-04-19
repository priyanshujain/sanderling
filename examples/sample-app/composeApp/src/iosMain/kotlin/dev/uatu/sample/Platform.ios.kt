package dev.uatu.sample

import kotlinx.cinterop.ExperimentalForeignApi
import platform.Foundation.NSDate
import platform.Foundation.NSDocumentDirectory
import platform.Foundation.NSFileManager
import platform.Foundation.NSSearchPathForDirectoriesInDomains
import platform.Foundation.NSString
import platform.Foundation.NSUserDomainMask
import platform.Foundation.NSUUID
import platform.Foundation.NSUTF8StringEncoding
import platform.Foundation.stringWithContentsOfFile
import platform.Foundation.timeIntervalSince1970
import platform.Foundation.writeToFile

@OptIn(ExperimentalForeignApi::class)
actual object Platform {
    private val dir: String by lazy {
        val docs = NSSearchPathForDirectoriesInDomains(
            NSDocumentDirectory, NSUserDomainMask, true,
        ).first() as String
        val ledger = "$docs/ledger"
        NSFileManager.defaultManager.createDirectoryAtPath(
            ledger, withIntermediateDirectories = true, attributes = null, error = null,
        )
        ledger
    }

    actual fun readFile(name: String): String? {
        val path = "$dir/$name"
        if (!NSFileManager.defaultManager.fileExistsAtPath(path)) return null
        @Suppress("CAST_NEVER_SUCCEEDS")
        return (NSString.stringWithContentsOfFile(
            path, encoding = NSUTF8StringEncoding, error = null,
        ) as String?)
    }

    @Suppress("CAST_NEVER_SUCCEEDS")
    actual fun writeFile(name: String, content: String) {
        val path = "$dir/$name"
        (content as NSString).writeToFile(
            path, atomically = true, encoding = NSUTF8StringEncoding, error = null,
        )
    }

    actual fun now(): Long = (NSDate().timeIntervalSince1970 * 1000.0).toLong()

    actual fun makeId(): String = NSUUID().UUIDString
}
