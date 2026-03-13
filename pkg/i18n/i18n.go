package i18n

import (
	"fmt"
	"os"
	"strings"
)

type Language string

const (
	English Language = "en"
	Russian Language = "ru"
)

var currentLang = English

// Translations map
var translations = map[Language]map[string]string{
	English: {
		// Common
		"package":        "package",
		"packages":       "packages",
		"building":       "Building",
		"installing":     "Installing",
		"removing":       "Removing",
		"searching":      "Searching",
		"downloading":    "Downloading",
		"extracting":     "Extracting",
		"configuring":    "Configuring",
		"compiling":      "Compiling",
		"completed":      "completed",
		"failed":         "failed",
		"success":        "success",
		"error":          "error",
		"warning":        "warning",
		"info":           "info",
		
		// Commands
		"build_cmd":      "Build a package from recipe",
		"install_cmd":    "Install package(s)",
		"remove_cmd":     "Remove installed package(s)",
		"list_cmd":       "List installed packages",
		"info_cmd":       "Show package information",
		"search_cmd":     "Search for packages",
		"doctor_cmd":     "Check system for potential issues",
		"cache_cmd":      "Manage build cache",
		
		// Status messages
		"preparing_dirs": "Preparing directories",
		"validating_src": "Validating source files",
		"applying_patches": "Applying recipe patches",
		"creating_archive": "Creating package archive",
		"package_built": "Package %s built successfully",
		"package_installed": "Package installation completed successfully",
		"packages_installed": "%d packages installed successfully",
		
		// Progress
		"step":           "Step",
		"of":             "of",
		"elapsed":        "elapsed",
		"remaining":      "remaining",
		"eta":            "ETA",
		"steps_per_sec":  "steps/s",
		
		// Doctor
		"system_info":    "System Information",
		"build_tools":    "Build Tools",
		"directories":    "Directories & Permissions",
		"network":        "Network Connectivity",
		"package_db":     "Package Database",
		"diagnosis_complete": "Diagnosis Complete",
		"fix_issues":     "If you see any ❌ items above, fix them before using zsvo.",
		
		// Cache
		"cache_info":     "Build Cache Information",
		"work_dir":       "Work directory",
		"total_cache":    "Total cache size",
		"cache_breakdown": "Cache breakdown",
		"download_cache": "Download cache",
		"built_packages": "Built packages",
		"source_files":   "Source files",
		"staging_files":  "Staging files",
		"cached_packages": "Cached packages",
		"cache_cleaned":  "Cache cleaned successfully",
		
		// Search
		"searching_for":  "Searching for packages matching",
		"no_packages_found": "No packages found matching",
		"found_packages": "Found %d packages",
		"showing_results": "showing first %d results, use --max-results to see more",
		"package_header": "PACKAGE",
		"version_header": "VERSION",
		"desc_header":    "DESCRIPTION",
		
		// Errors
		"recipe_not_found": "Recipe not found",
		"package_not_found": "Package not found",
		"build_failed":    "Build failed",
		"install_failed":  "Install failed",
		"network_error":   "Network error",
		"permission_error": "Permission error",
	},
	
	Russian: {
		// Common
		"package":        "кулёк",
		"packages":       "кульки",
		"building":       "Сборка",
		"installing":     "Установка",
		"removing":       "Удаление",
		"searching":      "Поиск",
		"downloading":    "Скачивание",
		"extracting":     "Распаковка",
		"configuring":    "Конфигурация",
		"compiling":      "Компиляция",
		"completed":      "завершено",
		"failed":         "провалено",
		"success":        "успешно",
		"error":          "ошибка",
		"warning":        "предупреждение",
		"info":           "инфо",
		
		// Commands
		"build_cmd":      "Собрать кульок из рецепта",
		"install_cmd":    "Установить кульки",
		"remove_cmd":     "Удалить установленные кульки",
		"list_cmd":       "Показать установленные кульки",
		"info_cmd":       "Информация о кульке",
		"search_cmd":     "Поиск кульков",
		"doctor_cmd":     "Проверка системы на проблемы",
		"cache_cmd":      "Управление кэшем сборки",
		
		// Status messages
		"preparing_dirs": "Подготовка директорий",
		"validating_src": "Проверка исходников",
		"applying_patches": "Применение патчей",
		"creating_archive": "Создание архива кулька",
		"package_built": "Кулёк %s успешно собран",
		"package_installed": "Установка кульков завершена успешно",
		"packages_installed": "%d кульков установлено успешно",
		
		// Progress
		"step":           "Шаг",
		"of":             "из",
		"elapsed":        "прошло",
		"remaining":      "осталось",
		"eta":            "Осталось",
		"steps_per_sec":  "шагов/сек",
		
		// Doctor
		"system_info":    "Информация о системе",
		"build_tools":    "Инструменты сборки",
		"directories":    "Директории и права",
		"network":        "Сетевое подключение",
		"package_db":     "База данных кульков",
		"diagnosis_complete": "Диагностика завершена",
		"fix_issues":     "Если видишь ❌ выше, исправь перед использованием zsvo.",
		
		// Cache
		"cache_info":     "Информация о кэше сборки",
		"work_dir":       "Рабочая директория",
		"total_cache":    "Общий размер кэша",
		"cache_breakdown": "Детализация кэша",
		"download_cache": "Кэш загрузок",
		"built_packages": "Собранные кульки",
		"source_files":   "Исходники",
		"staging_files":  "Временные файлы",
		"cached_packages": "Закэшированные кульки",
		"cache_cleaned":  "Кэш очищен успешно",
		
		// Search
		"searching_for":  "Поиск кульков по запросу",
		"no_packages_found": "Кульки не найдены по запросу",
		"found_packages": "Найдено кульков: %d",
		"showing_results": "показано первых %d, используй --max-results для больше",
		"package_header": "КУЛЁК",
		"version_header": "ВЕРСИЯ",
		"desc_header":    "ОПИСАНИЕ",
		
		// Errors
		"recipe_not_found": "Рецепт не найден",
		"package_not_found": "Кулёк не найден",
		"build_failed":    "Сборка провалена",
		"install_failed":  "Установка провалена",
		"network_error":   "Ошибка сети",
		"permission_error": "Ошибка прав доступа",
	},
}

// T translates text to current language
func T(key string, args ...interface{}) string {
	lang := currentLang
	
	if langTranslations, ok := translations[lang]; ok {
		if text, ok := langTranslations[key]; ok {
			if len(args) > 0 {
				return fmt.Sprintf(text, args...)
			}
			return text
		}
	}
	
	// Fallback to English
	if lang != English {
		if langTranslations, ok := translations[English]; ok {
			if text, ok := langTranslations[key]; ok {
				if len(args) > 0 {
					return fmt.Sprintf(text, args...)
				}
				return text
			}
		}
	}
	
	// Final fallback - return key
	if len(args) > 0 {
		return fmt.Sprintf(key, args...)
	}
	return key
}

// SetLanguage sets the current language
func SetLanguage(lang Language) {
	currentLang = lang
}

// GetLanguage returns the current language
func GetLanguage() Language {
	return currentLang
}

// DetectLanguage tries to detect language from environment
func DetectLanguage() {
	// Check environment variable
	if lang := os.Getenv("ZSVO_LANG"); lang != "" {
		switch strings.ToLower(lang) {
		case "ru", "russian":
			SetLanguage(Russian)
		case "en", "english":
			SetLanguage(English)
		}
		return
	}
	
	// Check system locale
	if lang := os.Getenv("LANG"); lang != "" {
		if strings.Contains(lang, "ru") || strings.Contains(lang, "RU") {
			SetLanguage(Russian)
			return
		}
	}
	
	// Default to English
	SetLanguage(English)
}

// IsRussian returns true if current language is Russian
func IsRussian() bool {
	return currentLang == Russian
}

// IsEnglish returns true if current language is English  
func IsEnglish() bool {
	return currentLang == English
}
