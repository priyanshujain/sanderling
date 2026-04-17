plugins {
    kotlin("jvm") version "2.1.21"
    application
    id("com.gradleup.shadow") version "9.0.0-rc2"
}

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
}

kotlin {
    jvmToolchain(17)
}

dependencies {
    testImplementation(kotlin("test"))
    testImplementation("org.junit.jupiter:junit-jupiter:5.11.3")
}

application {
    mainClass.set("dev.uatu.sidecar.MainKt")
}

tasks.test {
    useJUnitPlatform()
}
