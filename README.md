# uatu

Testing framework and spec in ts/js used for blackbox testing and property based testing


## Supported Platforms
- android
- ios

## Install

### CLI

Download the platform tarball from [GitHub Releases](https://github.com/priyanshujain/uatu/releases/latest):

```sh
# macOS arm64
curl -L https://github.com/priyanshujain/uatu/releases/latest/download/uatu_<version>_darwin_arm64.tar.gz | tar xz
# Linux amd64
curl -L https://github.com/priyanshujain/uatu/releases/latest/download/uatu_<version>_linux_amd64.tar.gz | tar xz

./uatu version
```

Pre-built for `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`.

### Spec API (npm)

```sh
npm install --save-dev @uatu/spec
```

```ts
import { extract, always, actions } from "@uatu/spec";
```

### Android SDK (Maven Central)

```kotlin
// settings.gradle.kts
dependencyResolutionManagement {
    repositories {
        mavenCentral()
    }
}

// app/build.gradle.kts
dependencies {
    implementation("io.github.priyanshujain:sdk-android:<version>")
}
```
