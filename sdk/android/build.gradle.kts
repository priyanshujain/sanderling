import com.vanniktech.maven.publish.AndroidSingleVariantLibrary
import com.vanniktech.maven.publish.JavadocJar
import com.vanniktech.maven.publish.SourcesJar

plugins {
    id("com.android.library") version "8.13.0"
    kotlin("android") version "2.1.21"
    id("com.vanniktech.maven.publish") version "0.36.0"
    id("org.jetbrains.dokka") version "2.2.0"
    id("org.jetbrains.dokka-javadoc") version "2.2.0"
}

version = findProperty("sanderling.version") as String? ?: "0.0.0-dev"
group = "io.github.priyanshujain"

android {
    namespace = "dev.sanderling.sdk"
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
}

mavenPublishing {
    publishToMavenCentral(automaticRelease = true)

    // Sign only when a release-signing key is provided (env or Gradle
    // property). Unsigned runs are useful for `publishToMavenLocal` dry-runs;
    // CI always has the key set so the actual Central push is always signed.
    if (findProperty("signingInMemoryKey") != null) {
        signAllPublications()
    }

    configure(
        AndroidSingleVariantLibrary(
            javadocJar = JavadocJar.Dokka("dokkaGeneratePublicationJavadoc"),
            sourcesJar = SourcesJar.Sources(),
            variant = "release",
        ),
    )

    coordinates(
        groupId = "io.github.priyanshujain",
        artifactId = "sdk-android",
        version = version.toString(),
    )

    pom {
        name.set("uatu sdk-android")
        description.set(
            "Android runtime SDK for uatu, a property-based UI fuzzer for mobile apps. " +
                "Exposes a content-provider accessibility bridge consumed by the uatu CLI at test time.",
        )
        url.set("https://github.com/priyanshujain/uatu")

        licenses {
            license {
                name.set("Apache License, Version 2.0")
                url.set("https://www.apache.org/licenses/LICENSE-2.0.txt")
                distribution.set("repo")
            }
        }

        developers {
            developer {
                id.set("priyanshujain")
                name.set("Priyanshu Jain")
                url.set("https://github.com/priyanshujain")
            }
        }

        scm {
            url.set("https://github.com/priyanshujain/uatu")
            connection.set("scm:git:git://github.com/priyanshujain/uatu.git")
            developerConnection.set("scm:git:ssh://git@github.com/priyanshujain/uatu.git")
        }
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
}
