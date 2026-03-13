package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"zsvo/pkg/i18n"
)

var LangCmd = &cobra.Command{
	Use:   "lang [language]",
	Short: "Set or display language",
	Long:  `Set interface language (en, ru) or display current language`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// Display current language
			current := i18n.GetLanguage()
			fmt.Printf("Current language: %s\n", current)
			
			// Show environment info
			if envLang := os.Getenv("ZSVO_LANG"); envLang != "" {
				fmt.Printf("ZSVO_LANG environment: %s\n", envLang)
			}
			if sysLang := os.Getenv("LANG"); sysLang != "" {
				fmt.Printf("System LANG: %s\n", sysLang)
			}
			
			fmt.Printf("\nAvailable languages:\n")
			fmt.Printf("  en - English\n")
			fmt.Printf("  ru - Русский\n")
			
			fmt.Printf("\nUsage:\n")
			fmt.Printf("  zsvo lang en    # Set English\n")
			fmt.Printf("  zsvo lang ru    # Установить русский\n")
			fmt.Printf("  ZSVO_LANG=ru zsvo install package  # Environment variable\n")
			
			return nil
		}
		
		lang := args[0]
		switch lang {
		case "en", "english":
			i18n.SetLanguage(i18n.English)
			fmt.Printf("Language set to English\n")
		case "ru", "russian":
			i18n.SetLanguage(i18n.Russian)
			fmt.Printf("Язык установлен на русский\n")
		default:
			return fmt.Errorf("unsupported language: %s. Use 'en' or 'ru'", lang)
		}
		
		// Test the language
		fmt.Printf("\nTest: %s\n", i18n.T("package"))
		fmt.Printf("Test: %s\n", i18n.T("building"))
		fmt.Printf("Test: %s\n", i18n.T("completed"))
		
		return nil
	},
}

func init() {
	// Will be added to root command in main.go
}
