#!/bin/bash

echo "🚀 Запускаем ZSVO в Docker..."

# Проверяем что Docker запущен
if ! docker ps > /dev/null 2>&1; then
    echo "❌ Docker не запущен. Запускаем Docker Desktop..."
    open -a Docker
    echo "⏳ Ждем запуска Docker..."
    sleep 10
fi

# Запускаем контейнер
docker run -it --rm \
    -v $(pwd):/workspace \
    -w /workspace \
    --name zsvo-test \
    golang:1.23 bash -c "
    echo '=== УСТАНАВЛИВАЕМ ЗАВИСИМОСТИ ===' &&
    apt-get update -qq &&
    apt-get install -y -qq build-essential git curl wget cmake meson pkg-config python3 tar xz-utils &&

    echo '=== СОБИРАЕМ ZSVO ===' &&
    go build -o zsvo . &&

    echo '=== ГОТОВО! ТЕРМИНАЛ ZSVO ===' &&
    echo 'Доступные команды:' &&
    echo '  ./zsvo doctor' &&
    echo '  ./zsvo install htop --dry-run' &&
    echo '  ./zsvo install neovim --dry-run' &&
    echo '  ./zsvo search python' &&
    echo '  ./zsvo lang ru' &&
    echo '  ./zsvo cache info' &&
    echo '' &&
    echo 'Для выхода: exit' &&
    echo '' &&
    bash
"

echo "✅ ZSVO Docker контейнер завершен"
