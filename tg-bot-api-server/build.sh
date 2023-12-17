#!/usr/bin/env sh

# Exit on first error.
set -e

# You can copy/paste build script from telegram website by following link:
# https://tdlib.github.io/telegram-bot-api/build.html?os=Linux
# Also, don't forget to choose to build from root user because we don't have sudo in docker.

apk update
apk upgrade
apk add --update alpine-sdk linux-headers git zlib-dev openssl-dev gperf cmake
git clone --recursive https://github.com/tdlib/telegram-bot-api.git
cd telegram-bot-api
rm -rf build
mkdir build
cd build
cmake -DCMAKE_BUILD_TYPE=Debug -DCMAKE_INSTALL_PREFIX:PATH=.. ..
cmake --build . --target install
cd ../..
ls -l telegram-bot-api/bin/telegram-bot-api*
