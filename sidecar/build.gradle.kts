import com.google.protobuf.gradle.id

plugins {
    kotlin("jvm") version "2.1.21"
    application
    id("com.gradleup.shadow") version "9.0.0-rc2"
    id("com.google.protobuf") version "0.9.5"
}

version = findProperty("sanderling.version") as String? ?: "0.0.0-dev"

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
}

kotlin {
    jvmToolchain(17)
}

val grpcVersion = "1.68.0"
val protobufVersion = "3.25.5"
val maestroVersion = "1.40.0"

dependencies {
    implementation("dev.mobile:maestro-client:$maestroVersion")

    implementation("io.grpc:grpc-netty-shaded:$grpcVersion")
    implementation("io.grpc:grpc-protobuf:$grpcVersion")
    implementation("io.grpc:grpc-stub:$grpcVersion")
    implementation("com.google.protobuf:protobuf-java:$protobufVersion")
    implementation("javax.annotation:javax.annotation-api:1.3.2")

    implementation("org.slf4j:slf4j-simple:2.0.16")

    testImplementation(kotlin("test"))
    testImplementation("io.grpc:grpc-testing:$grpcVersion")
    testImplementation("io.grpc:grpc-inprocess:$grpcVersion")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.junit.jupiter:junit-jupiter:5.11.3")
    testRuntimeOnly("org.junit.vintage:junit-vintage-engine:5.11.3")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher:1.11.3")
}

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:$protobufVersion"
    }
    plugins {
        id("grpc") {
            artifact = "io.grpc:protoc-gen-grpc-java:$grpcVersion"
        }
    }
    generateProtoTasks {
        all().forEach { task ->
            task.plugins {
                id("grpc")
            }
        }
    }
}

sourceSets {
    main {
        proto {
            srcDir(rootProject.file("proto"))
        }
    }
}

application {
    mainClass.set("dev.sanderling.sidecar.MainKt")
}

// The Go binary embeds this fat JAR by fixed path (internal/sidecar/assets/
// sidecar-all.jar). Pin the shadow output name so the embed step doesn't
// depend on `version`.
tasks.named<com.github.jengelman.gradle.plugins.shadow.tasks.ShadowJar>("shadowJar") {
    archiveFileName.set("sidecar-all.jar")
}

// JUnit 4 (vintage) for the gRPC GrpcCleanupRule + JUnit 5 (jupiter)
// for the rest. UseJUnitPlatform with the vintage engine handles both.
tasks.test {
    useJUnitPlatform()
}
