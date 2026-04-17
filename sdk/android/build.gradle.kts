plugins {
    id("com.android.library") version "8.11.0"
    kotlin("android") version "2.1.21"
    `maven-publish`
}

android {
    namespace = "dev.uatu.sdk"
    compileSdk = 35

    defaultConfig {
        minSdk = 24
        consumerProguardFiles("consumer-rules.pro")
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }
}

publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "dev.uatu"
            artifactId = "sdk-android"
            version = "0.0.1"
            afterEvaluate {
                from(components["release"])
            }
        }
    }
    repositories {
        maven {
            name = "GitHubPackages"
            url = uri("https://maven.pkg.github.com/priyanshujain/uatu")
            credentials {
                username = System.getenv("GH_USERNAME") ?: System.getenv("GITHUB_ACTOR") ?: "priyanshujain"
                password = System.getenv("GH_TOKEN") ?: System.getenv("GITHUB_TOKEN") ?: ""
            }
        }
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
}
