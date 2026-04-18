pluginManagement {
    repositories {
        gradlePluginPortal()
        google()
        mavenCentral()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "uatu"

include(":sidecar")
include(":sdk-android")
project(":sdk-android").projectDir = file("sdk/android")
include(":sample-app")
project(":sample-app").projectDir = file("examples/sample-app/android")
