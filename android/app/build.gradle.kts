import java.util.Properties

plugins {
    id("com.android.application")
}

// Ключ подписи живёт вне репозитория и не попадает в него никогда.
//
// Подпись — это личность приложения на всю жизнь: телефон принимает обновление
// только за той же подписью, что и установленную версию. Потеряешь ключ — и
// обновить приложение у покупателей будет нечем, придётся просить их удалить
// старое и поставить новое как чужое. Утечёт — кто угодно выпустит «обновление»
// от твоего имени, и телефоны его примут.
//
// Путь задаётся переменной VEIL_RELEASE_KEYS, по умолчанию — каталог veil-keys
// рядом с репозиторием. Файла нет — сборка release выйдет неподписанной и
// честно об этом скажет, вместо того чтобы молча подписаться отладочным
// ключом.
val releaseKeysFile = file(
    System.getenv("VEIL_RELEASE_KEYS")
        ?: rootProject.file("../../veil-keys/veil-release.properties").path,
)

val releaseKeys = Properties().apply {
    if (releaseKeysFile.exists()) {
        releaseKeysFile.inputStream().use { load(it) }
    } else {
        logger.warn("ключ подписи не найден: $releaseKeysFile — сборка release будет неподписанной")
    }
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

    signingConfigs {
        if (!releaseKeys.isEmpty) {
            create("release") {
                storeFile = file(releaseKeys.getProperty("storeFile"))
                storePassword = releaseKeys.getProperty("storePassword")
                keyAlias = releaseKeys.getProperty("keyAlias")
                keyPassword = releaseKeys.getProperty("keyPassword")

                // v3 включается явно. Она единственная позволяет однажды
                // сменить ключ подписи, не теряя установленные приложения, —
                // а ключ живёт тридцать лет, и за это время всякое бывает.
                // v1 не нужна: это подпись для Android 6 и старше, а мы
                // начинаем с седьмого.
                enableV1Signing = false
                enableV2Signing = true
                enableV3Signing = true
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")

            // Сжатие кода выключено намеренно, и это не забывчивость.
            //
            // Выигрыш от него здесь почти нулевой: из шестнадцати мегабайт
            // пакета пятнадцать — это ядро на Go, скомпилированное в машинный
            // код, до которого R8 не дотягивается вовсе. Сжать он может от силы
            // сотню-другую килобайт байткода.
            //
            // А риск настоящий: классы моста вызываются через JNI, статический
            // анализ обращений к ним не видит и выбрасывает их как ненужные.
            // Правила в proguard-rules.pro это чинят, но ошибка в них
            // проявляется не на сборке, а падением уже у покупателя.
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
