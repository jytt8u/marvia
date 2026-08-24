plugins {
    id("com.android.application")
}

android {
    namespace = "io.veil.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.veil.android"
        minSdk = 24
        targetSdk = 36
        versionCode = 1
        versionName = "0.1"

        // Только ARM. На x86 работают эмуляторы и несколько редких планшетов,
        // ради которых пакет вырос бы в полтора раза: ядро на Go весит около
        // двадцати мегабайт на каждую архитектуру.
        ndk {
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
    }

    buildTypes {
        release {
            // Сжатие кода выключено намеренно. Ядро приходит библиотекой,
            // собранной gomobile, и R8 не видит, что её классы вызываются
            // через JNI: он их выбрасывает, а приложение падает уже у
            // покупателя. Правила в proguard-rules.pro это чинят, но включать
            // сжатие имеет смысл вместе с настоящей подписью.
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        viewBinding = true
    }

    packaging {
        jniLibs {
            // Ядро кладём в пакет сжатым. Несжатое запускалось бы чуть быстрее
            // и не занимало место дважды, но пакет весил бы сорок мегабайт
            // вместо семнадцати. Его качают через телеграм-бота, часто с
            // мобильного интернета и часто в стране, где он дорогой.
            useLegacyPackaging = true
        }
    }
}


dependencies {
    // Ядро: тот же код на Go, что работает на сервере и на настольной машине.
    implementation(files("libs/veil.aar"))

    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.activity:activity-ktx:1.9.3")
    implementation("androidx.constraintlayout:constraintlayout:2.2.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("com.google.android.material:material:1.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
}
