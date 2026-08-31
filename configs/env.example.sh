#!/bin/sh
# Секреты сервиса. Скопировать в env.sh рядом с бинарником, заполнить,
# выставить права 0600 и подключать перед запуском: source ./env.sh
#
# В config.yaml этих значений нет намеренно: конфиг можно коммитить,
# env.sh — нельзя, он под .gitignore.

# https://my.telegram.org -> API development tools
export TELEGRAM_API_ID=20789316
export TELEGRAM_API_HASH=9bb5379bbe12e9960bc4ced3328e09d0


# Телефон аккаунта, который состоит в канале. В международном формате.
export TELEGRAM_PHONE=+77478835039

# Только для -login и только если на аккаунте включена двухфакторная защита.
# Для обычных запусков не нужен.
# export TELEGRAM_2FA_PASSWORD=

# Клиентский файл OAuth типа Desktop app, скачанный из Google Cloud Console.
export GOOGLE_OAUTH_CLIENT="$PWD/secrets/client_secret.json"
