plugins {
    id("com.android.application") version "8.11.0"
    kotlin("android") version "2.1.21"
}

val uatuVersion = findProperty("uatu.version") as String? ?: "0.0.0-dev"

android {
    namespace = "dev.uatu.sample"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.uatu.sample"
        minSdk = 24
        targetSdk = 35
        versionCode = 1
        versionName = uatuVersion
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildTypes {
        debug {
            isDebuggable = true
        }
    }
}

dependencies {
    implementation("io.github.priyanshujain:sdk-android:$uatuVersion")
}
