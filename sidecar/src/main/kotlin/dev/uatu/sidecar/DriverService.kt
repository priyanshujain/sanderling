package dev.uatu.sidecar

import com.google.protobuf.ByteString
import dev.uatu.driver.v1.DriverGrpc
import dev.uatu.driver.v1.Duration
import dev.uatu.driver.v1.Empty
import dev.uatu.driver.v1.HealthStatus
import dev.uatu.driver.v1.HierarchyJSON
import dev.uatu.driver.v1.Image
import dev.uatu.driver.v1.LaunchRequest
import dev.uatu.driver.v1.Point
import dev.uatu.driver.v1.Selector
import dev.uatu.driver.v1.Text
import io.grpc.stub.StreamObserver
import java.util.concurrent.atomic.AtomicReference

class DriverService(
    private val platform: String = "android",
    private val serial: String? = null,
    private val backend: DriverBackend = StubDriverBackend(platform),
) : DriverGrpc.DriverImplBase() {

    private val launchedBundleId = AtomicReference<String?>(null)

    override fun launch(request: LaunchRequest, responseObserver: StreamObserver<Empty>) {
        runRpc(responseObserver) {
            backend.launch(request.bundleId, request.launcherActivity, request.clearState)
            launchedBundleId.set(request.bundleId)
            Empty.getDefaultInstance()
        }
    }

    override fun terminate(request: Empty, responseObserver: StreamObserver<Empty>) {
        runRpc(responseObserver) {
            launchedBundleId.get()?.let { backend.terminate(it) }
            launchedBundleId.set(null)
            Empty.getDefaultInstance()
        }
    }

    override fun tap(request: Point, responseObserver: StreamObserver<Empty>) {
        runRpc(responseObserver) {
            backend.tap(request.x, request.y)
            Empty.getDefaultInstance()
        }
    }

    override fun tapSelector(request: Selector, responseObserver: StreamObserver<Empty>) {
        runRpc(responseObserver) {
            backend.tapSelector(request.value)
            Empty.getDefaultInstance()
        }
    }

    override fun inputText(request: Text, responseObserver: StreamObserver<Empty>) {
        runRpc(responseObserver) {
            backend.inputText(request.value)
            Empty.getDefaultInstance()
        }
    }

    override fun screenshot(request: Empty, responseObserver: StreamObserver<Image>) {
        runRpc(responseObserver) {
            val (png, width, height) = backend.screenshot()
            Image.newBuilder()
                .setPng(ByteString.copyFrom(png))
                .setWidth(width)
                .setHeight(height)
                .build()
        }
    }

    override fun hierarchy(request: Empty, responseObserver: StreamObserver<HierarchyJSON>) {
        runRpc(responseObserver) {
            HierarchyJSON.newBuilder().setJson(backend.hierarchy()).build()
        }
    }

    override fun waitForIdle(request: Duration, responseObserver: StreamObserver<Empty>) {
        runRpc(responseObserver) {
            backend.waitForIdle(request.millis)
            Empty.getDefaultInstance()
        }
    }

    override fun health(request: Empty, responseObserver: StreamObserver<HealthStatus>) {
        runRpc(responseObserver) {
            HealthStatus.newBuilder()
                .setReady(backend.healthy())
                .setVersion(VERSION)
                .setPlatform(platform)
                .build()
        }
    }

    private inline fun <T> runRpc(observer: StreamObserver<T>, block: () -> T) {
        try {
            observer.onNext(block())
            observer.onCompleted()
        } catch (cause: Exception) {
            observer.onError(cause)
        }
    }

    companion object {
        const val VERSION: String = "0.0.1"
    }
}
