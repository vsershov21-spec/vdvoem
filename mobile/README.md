# «Вдвоём» для iPhone и Android

Здесь две нативные оболочки (Capacitor 7). Внутри каждой тот же интерфейс, что в Telegram: оболочка открывает
`https://vdvoem.vershov.com/app/`, поэтому приложение обновляется вместе с сервером, без новой сборки.
Нет связи: показывается страница `www/index.html` с кнопкой «Попробовать снова».

Вход вне Telegram: кнопка «Войти через Telegram» (бот спросит «Это ты?»), Face ID или логин с паролем.

## iPhone (Xcode)

1. Открой `mobile/ios/App/App.xcodeproj` в Xcode 15 или новее. Пакеты Capacitor Xcode скачает сам (Swift Package Manager, CocoaPods не нужен).
2. Target **App → Signing & Capabilities**: выбери свою команду (Team). Идентификатор сейчас `com.vershov.vdvoem`, его можно поменять там же.
3. Выбери свой iPhone и нажми Run. Для TestFlight: Product → Archive → Distribute App.

Node на Mac не нужен: всё, что создаёт `cap sync`, уже лежит в репозитории. Он понадобится только после правок
`capacitor.config.json` или `www/`: `cd mobile && npm install && npx cap sync`.

## Android (Android Studio)

1. Открой папку `mobile/android` в Android Studio, дождись синхронизации Gradle.
2. Run на телефоне или Build → Build APK(s). Файл: `android/app/build/outputs/apk/debug/app-debug.apk`.
3. Для подписанного APK или магазина: Build → Generate Signed Bundle / APK, свой ключ.

## Что настроено

- Имя «Вдвоём», иконка и заставка с аркой, тёмный фон `#0b0b0d`.
- Только портретная ориентация.
- Микрофон (голос в помощнике): описание для iOS, разрешение `RECORD_AUDIO` для Android.
- Android: системная «Назад» ходит по экранам приложения, с первого экрана закрывает его.

## Что не проверено

Проекты созданы и настроены на Linux, где нет ни Xcode, ни Android SDK: сборка и запуск на устройстве не проверялись.
Отдельно стоит проверить на телефоне: вход через Telegram (ссылка должна открыться в Telegram и вернуть в приложение),
вход по Face ID (для ключей входа в нативном приложении может понадобиться Associated Domains), голос, отступы под
вырез экрана.

App Store: Apple часто отклоняет приложения, которые только показывают сайт (правило 4.2). Для установки через Xcode
и для TestFlight это не мешает. Для публикации в магазине, скорее всего, понадобятся нативные возможности, например
push-уведомления.
