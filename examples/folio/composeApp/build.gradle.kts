import org.jetbrains.kotlin.gradle.ExperimentalKotlinGradlePluginApi
import org.jetbrains.kotlin.gradle.ExperimentalWasmDsl
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    kotlin("multiplatform")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.compose")
    id("app.cash.sqldelight")
}

val sanderlingVersion = findProperty("sanderling.version") as String? ?: "0.0.0-dev"
val sqldelightVersion = "2.3.2"

kotlin {
    @OptIn(ExperimentalKotlinGradlePluginApi::class)
    compilerOptions {
        freeCompilerArgs.add("-Xexpect-actual-classes")
        optIn.add("kotlin.js.ExperimentalWasmJsInterop")
    }

    androidTarget {
        @OptIn(ExperimentalKotlinGradlePluginApi::class)
        compilerOptions {
            jvmTarget.set(JvmTarget.JVM_17)
        }
    }

    listOf(
        iosX64(),
        iosArm64(),
        iosSimulatorArm64(),
    ).forEach { target ->
        target.binaries.framework {
            baseName = "ComposeApp"
            isStatic = true
            binaryOption("bundleId", "app.folio")
        }
    }

    @OptIn(ExperimentalWasmDsl::class)
    wasmJs {
        outputModuleName.set("composeApp")
        browser {
            commonWebpackConfig {
                outputFileName = "composeApp.js"
            }
        }
        binaries.executable()
    }

    applyHierarchyTemplate {
        common {
            group("sql") {
                withAndroidTarget()
                group("apple") {
                    group("ios") {
                        withIosX64()
                        withIosArm64()
                        withIosSimulatorArm64()
                    }
                }
            }
            group("wasmJs") {
                withWasmJs()
            }
        }
    }

    sourceSets {
        commonMain.dependencies {
            implementation(compose.runtime)
            implementation(compose.foundation)
            implementation(compose.material3)
            implementation(compose.ui)
            implementation(compose.components.resources)
            implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2")
            implementation("app.cash.sqldelight:runtime:$sqldelightVersion")
            implementation("app.cash.sqldelight:coroutines-extensions:$sqldelightVersion")
        }

        androidMain.dependencies {
            implementation("androidx.activity:activity-compose:1.13.0")
            implementation("app.cash.sqldelight:android-driver:$sqldelightVersion")
            implementation("io.github.priyanshujain.sanderling:sdk-android:$sanderlingVersion")
        }

        iosMain.dependencies {
            implementation("app.cash.sqldelight:native-driver:$sqldelightVersion")
        }
    }
}

android {
    namespace = "app.folio"
    compileSdk = 36

    defaultConfig {
        applicationId = "app.folio"
        minSdk = 24
        targetSdk = 36
        versionCode = 1
        versionName = sanderlingVersion
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildTypes {
        debug {
            isDebuggable = true
        }
    }

    sourceSets["main"].apply {
        manifest.srcFile("src/androidMain/AndroidManifest.xml")
        res.srcDirs("src/androidMain/res")
    }
}

sqldelight {
    databases {
        create("LedgerDatabase") {
            packageName.set("app.folio.db")
        }
    }
}
