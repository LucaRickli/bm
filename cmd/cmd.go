package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "bm",
	Short: "Binary manager",
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initLogging()
	},
}

func init() {
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (trace, debug, info, warn, error)")
	if err := viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level")); err != nil {
		panic(err)
	}

	viper.SetEnvPrefix("BM")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()
}

func initLogging() {
	levelStr := viper.GetString("log-level")

	level, err := zerolog.ParseLevel(levelStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid log level %q, defaulting to info\n", levelStr)
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)

	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	})

	log.Debug().Str("log_level", level.String()).Msg("Logger initialized")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
