# Dokka / Java 25 publish failure

## What fails

Running `./gradlew :sdk-android:publishToMavenLocal` (or any task that triggers `javaDocReleaseGeneration`) on a JDK 25 host crashes with:

```
Execution failed for task ':sdk-android:javaDocReleaseGeneration'.
> A failure occurred while executing com.android.build.gradle.tasks.JavaDocGenerationTask$DokkaWorkAction
...
Caused by: java.lang.IllegalArgumentException: 25.0.2
    at com.intellij.util.lang.JavaVersion.parse(JavaVersion.java:298)
```

## Why it happens

The publish flow is:

1. `com.vanniktech.maven.publish:0.30.0` (declared in `sdk/android/build.gradle.kts:7`) configures an `AndroidSingleVariantLibrary` with `publishJavadocJar = true`.
2. That wires AGP's `JavaDocGenerationTask` into the release publication. The task runs Dokka in a `WorkAction`.
3. Dokka ships a vendored copy of IntelliJ's platform SDK. When it boots, it probes the JDK version by calling `com.intellij.util.lang.JavaVersion.parse(System.getProperty("java.version"))`.
4. On the host JDK the property is literally the string `"25.0.2"`. Dokka's bundled `JavaVersion.parse` (an older IntelliJ build) only understands the legacy patterns (`1.8.0_x`, `9`, `11.0.x`, etc.) and refuses version 25's format, throwing `IllegalArgumentException: 25.0.2`.
5. The worker dies, Gradle marks `javaDocReleaseGeneration` failed, and the whole publish lifecycle aborts before any POM or AAR is written.

## Why it's latent in CI

CI runs on JDK 17 per `build.gradle.kts` compile targets and whatever `actions/setup-java` installs. `"17.0.x"` matches the old format, so the bundled `JavaVersion.parse` doesn't blow up. The bug only surfaces on developer machines running JDK 21+ where the version string format changed, and reliably on JDK 25.

## Workaround

```sh
./gradlew :sdk-android:publishToMavenLocal -x javaDocReleaseGeneration
```

The AAR, sources jar, and POM publish cleanly; only the Javadoc jar is skipped. That produces a usable `0.0.0-dev` artifact for local testing but is not a substitute for a real release publish, since Maven Central validation requires the Javadoc jar.

## Possible fixes

1. **Bump the `vanniktech.maven.publish` plugin to a version that ships a newer Dokka.** The `0.30.0` version pinned here is from mid-2024. Newer releases (0.33+) bundle Dokka 2.x, which has rewritten the JDK detection path and handles modern version strings. One line in `sdk/android/build.gradle.kts`, no build config change. Requires verifying the plugin's breaking changes for the publish config.

2. **Pin a compatible Dokka directly.** Add `id("org.jetbrains.dokka") version "1.9.20"` (or later) so the plugin picks up Dokka's own fix for IntelliJ `JavaVersion.parse`. Slightly more invasive; you now manage Dokka independently from the publish plugin.

3. **Force a JDK toolchain for the javadoc task.** Add a `java { toolchain { languageVersion = JavaLanguageVersion.of(17) } }` block scoped to the SDK module (or use `tasks.withType<JavaDocGenerationTask> { javaLauncher.set(...) }`). Gradle downloads JDK 17 and runs Dokka under it regardless of the host JDK. Robust but adds a toolchain dependency to every dev machine's first build.

4. **Don't generate a Javadoc jar.** Flip `publishJavadocJar = false` in the vanniktech config. Kills the problem by removing the failing path. Not viable for Maven Central release (they require a Javadoc jar).

## Recommendation

Option 1. Smallest diff, addresses the root cause, keeps the publish pipeline intact for Central releases, and newer plugin versions also bring AGP 8.x compatibility improvements.
